package rollcall

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// DefaultPort is the TCP port RollCall control traffic uses on a frame/gateway.
// Port 2050 is the full RollCall network; some gateways also expose 2051 for the
// local chassis only. Override with WithPort.
const DefaultPort = 2050

// DefaultMaxInFlight bounds how many requests may be outstanding on the single
// connection at once. The reference RollCall Control Panel was observed issuing
// requests strictly one-at-a-time (max 1 in-flight), so the safe default is 1.
// Raise it with WithMaxInFlight once you've confirmed the frame tolerates
// pipelining (set 0 for unbounded — not recommended until tested on hardware).
const DefaultMaxInFlight = 1

// Client is a RollCall client multiplexed over a single TCP connection to one
// frame. It is safe for concurrent use: many goroutines may call Get/Set at once.
//
// A background reader goroutine frames inbound messages and routes replies to the
// matching in-flight request (keyed by source address + command id). Unsolicited
// updates (REPLY with FlagNotify, or any reply with no waiter) are delivered on
// the Notify channel.
type Client struct {
	conn net.Conn
	br   *bufio.Reader

	self Addr // our source address used on outgoing requests

	wmu sync.Mutex // serialises writes to conn

	mu         sync.Mutex
	pending    map[replyKey][]chan Message
	ackWaiters map[Addr][]chan struct{}

	sem chan struct{} // bounds in-flight requests; nil == unbounded

	notifyCh chan Message

	closeOnce sync.Once
	done      chan struct{}
	errMu     sync.Mutex
	readErr   error

	writeTimeout time.Duration
}

type replyKey struct {
	src Addr
	cmd uint32
}

// Option configures a Client.
type Option func(*config)

type config struct {
	port         int
	self         Addr
	notifyBuf    int
	writeTimeout time.Duration
	maxInFlight  int
	dialer       *net.Dialer
}

// WithPort overrides the TCP port (default DefaultPort).
func WithPort(p int) Option { return func(c *config) { c.port = p } }

// WithSelf sets the client's own RollCall source address.
func WithSelf(a Addr) Option { return func(c *config) { c.self = a } }

// WithNotifyBuffer sets the buffer size of the Notify channel (default 64).
func WithNotifyBuffer(n int) Option { return func(c *config) { c.notifyBuf = n } }

// WithWriteTimeout bounds how long a single write may block (default 5s).
func WithWriteTimeout(d time.Duration) Option { return func(c *config) { c.writeTimeout = d } }

// WithDialer supplies a custom net.Dialer for Dial.
func WithDialer(d *net.Dialer) Option { return func(c *config) { c.dialer = d } }

// WithMaxInFlight bounds outstanding requests on the connection (default
// DefaultMaxInFlight). Use 0 for unbounded.
func WithMaxInFlight(n int) Option { return func(c *config) { c.maxInFlight = n } }

func defaults() *config {
	return &config{
		port:         DefaultPort,
		self:         Addr{Net: 0, Unit: 0, Port: 2},
		notifyBuf:    64,
		writeTimeout: 5 * time.Second,
		maxInFlight:  DefaultMaxInFlight,
		dialer:       &net.Dialer{},
	}
}

// Dial connects to a frame at frameIP and returns a ready Client.
func Dial(ctx context.Context, frameIP string, opts ...Option) (*Client, error) {
	cfg := defaults()
	for _, o := range opts {
		o(cfg)
	}
	addr := net.JoinHostPort(frameIP, strconv.Itoa(cfg.port))
	conn, err := cfg.dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return newClient(conn, cfg), nil
}

// NewConn wraps an already-established connection (e.g. for testing). It takes
// ownership of conn and starts the reader goroutine.
func NewConn(conn net.Conn, opts ...Option) *Client {
	cfg := defaults()
	for _, o := range opts {
		o(cfg)
	}
	return newClient(conn, cfg)
}

func newClient(conn net.Conn, cfg *config) *Client {
	c := &Client{
		conn:         conn,
		br:           bufio.NewReader(conn),
		self:         cfg.self,
		pending:      make(map[replyKey][]chan Message),
		ackWaiters:   make(map[Addr][]chan struct{}),
		notifyCh:     make(chan Message, cfg.notifyBuf),
		done:         make(chan struct{}),
		writeTimeout: cfg.writeTimeout,
	}
	if cfg.maxInFlight > 0 {
		c.sem = make(chan struct{}, cfg.maxInFlight)
	}
	go c.readLoop()
	return c
}

// acquire takes an in-flight slot, respecting ctx and connection close.
func (c *Client) acquire(ctx context.Context) error {
	if c.sem == nil {
		return nil
	}
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.closedErr()
	}
}

func (c *Client) release() {
	if c.sem != nil {
		<-c.sem
	}
}

// Self returns the client's source address.
func (c *Client) Self() Addr { return c.self }

// Notify delivers unsolicited value updates pushed by the device.
func (c *Client) Notify() <-chan Message { return c.notifyCh }

// Get reads a single parameter from a unit and waits for the reply.
func (c *Client) Get(ctx context.Context, unit Addr, cmdID uint32) (Value, error) {
	if err := c.acquire(ctx); err != nil {
		return Value{}, err
	}
	defer c.release()

	ch := c.register(unit, cmdID)
	defer c.unregister(unit, cmdID, ch)

	if err := c.write(Message{Dst: unit, Src: c.self, Opcode: OpGet, CmdID: cmdID}); err != nil {
		return Value{}, err
	}
	return c.await(ctx, ch)
}

// BatchGet reads many parameters from one unit, honouring the in-flight limit.
// It returns the values that succeeded and a per-command error map for those
// that failed, so one unreadable parameter does not abort the whole batch.
func (c *Client) BatchGet(ctx context.Context, unit Addr, cmdIDs []uint32) (map[uint32]Value, map[uint32]error) {
	values := make(map[uint32]Value, len(cmdIDs))
	errs := make(map[uint32]error)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, cmd := range cmdIDs {
		wg.Add(1)
		go func(cmd uint32) {
			defer wg.Done()
			v, err := c.Get(ctx, unit, cmd) // pacing enforced by the in-flight semaphore
			mu.Lock()
			if err != nil {
				errs[cmd] = err
			} else {
				values[cmd] = v
			}
			mu.Unlock()
		}(cmd)
	}
	wg.Wait()
	return values, errs
}

// Set writes a parameter to a unit and waits for the device's reply (which echoes
// the resulting value). Use SetNoWait for fire-and-forget.
func (c *Client) Set(ctx context.Context, unit Addr, cmdID uint32, v Value) (Value, error) {
	if err := c.acquire(ctx); err != nil {
		return Value{}, err
	}
	defer c.release()

	ch := c.register(unit, cmdID)
	defer c.unregister(unit, cmdID, ch)

	if err := c.write(Message{Dst: unit, Src: c.self, Opcode: OpSet, CmdID: cmdID, Value: v}); err != nil {
		return Value{}, err
	}
	return c.await(ctx, ch)
}

// Open attaches to (subscribes) a unit and waits for the frame's ACK.
//
// In captures the reference client OPENs each unit it watches and re-OPENs it
// about every 10s (a keepalive / re-attach). I/O does NOT require a prior Open —
// GET/SET to an un-OPENed unit was observed to work — but a long-lived connection
// likely needs periodic Opens to stay attached. Drive that from your per-frame
// owner on a ~10s ticker; reconnect/backoff is intentionally the owner's job too
// (the Client closes on the first read error and does not auto-redial).
func (c *Client) Open(ctx context.Context, unit Addr) error {
	ch := make(chan struct{}, 1)
	c.mu.Lock()
	c.ackWaiters[unit] = append(c.ackWaiters[unit], ch)
	c.mu.Unlock()
	defer c.removeAck(unit, ch)

	if err := c.write(Message{Dst: unit, Src: c.self, Opcode: OpOpen}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.closedErr()
	case <-ch:
		return nil
	}
}

func (c *Client) removeAck(unit Addr, ch chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.ackWaiters[unit]
	for i, w := range q {
		if w == ch {
			c.ackWaiters[unit] = append(q[:i], q[i+1:]...)
			break
		}
	}
	if len(c.ackWaiters[unit]) == 0 {
		delete(c.ackWaiters, unit)
	}
}

func (c *Client) dispatchAck(src Addr) {
	c.mu.Lock()
	q := c.ackWaiters[src]
	if len(q) == 0 {
		c.mu.Unlock()
		return
	}
	ch := q[0]
	c.ackWaiters[src] = q[1:]
	if len(c.ackWaiters[src]) == 0 {
		delete(c.ackWaiters, src)
	}
	c.mu.Unlock()
	select {
	case ch <- struct{}{}:
	default:
	}
}

// SetNoWait writes a parameter without waiting for a reply.
func (c *Client) SetNoWait(unit Addr, cmdID uint32, v Value) error {
	return c.write(Message{Dst: unit, Src: c.self, Opcode: OpSet, CmdID: cmdID, Value: v})
}

// Take fires a "take"/commit command (a SET of the take command id to 1), which
// the device uses to apply a batch of staged settings.
func (c *Client) Take(ctx context.Context, unit Addr, takeCmd uint32) error {
	_, err := c.Set(ctx, unit, takeCmd, Int(1))
	return err
}

// Send transmits an arbitrary message (handshake, OPEN, IDENT, ...) without
// waiting for a reply. Exposed for protocol experimentation / session setup.
func (c *Client) Send(m Message) error { return c.write(m) }

func (c *Client) await(ctx context.Context, ch chan Message) (Value, error) {
	select {
	case <-ctx.Done():
		return Value{}, ctx.Err()
	case <-c.done:
		return Value{}, c.closedErr()
	case m := <-ch:
		return m.Value, nil
	}
}

func (c *Client) register(unit Addr, cmdID uint32) chan Message {
	ch := make(chan Message, 1)
	k := replyKey{src: unit, cmd: cmdID}
	c.mu.Lock()
	c.pending[k] = append(c.pending[k], ch)
	c.mu.Unlock()
	return ch
}

func (c *Client) unregister(unit Addr, cmdID uint32, ch chan Message) {
	k := replyKey{src: unit, cmd: cmdID}
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.pending[k]
	for i, w := range q {
		if w == ch {
			c.pending[k] = append(q[:i], q[i+1:]...)
			break
		}
	}
	if len(c.pending[k]) == 0 {
		delete(c.pending, k)
	}
}

// dispatch delivers m to one waiter for its (Src, CmdID); returns true if matched.
func (c *Client) dispatch(m Message) bool {
	k := replyKey{src: m.Src, cmd: m.CmdID}
	c.mu.Lock()
	q := c.pending[k]
	if len(q) == 0 {
		c.mu.Unlock()
		return false
	}
	ch := q[0]
	c.pending[k] = q[1:]
	if len(c.pending[k]) == 0 {
		delete(c.pending, k)
	}
	c.mu.Unlock()

	select {
	case ch <- m:
	default: // waiter already satisfied/abandoned
	}
	return true
}

func (c *Client) write(m Message) error {
	b := m.Encode()
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.writeTimeout > 0 {
		_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}
	_, err := c.conn.Write(b)
	return err
}

func (c *Client) readLoop() {
	for {
		m, err := readMessage(c.br)
		if err != nil {
			c.fail(err)
			return
		}
		switch m.Opcode {
		case OpReply:
			// Satisfy an in-flight GET/SET if one is waiting; otherwise it's an
			// unsolicited update — hand it to whoever is draining Notify.
			if !c.dispatch(m) {
				select {
				case c.notifyCh <- m:
				default: // drop if nobody is draining notifies
				}
			}
		case OpAck:
			// Acknowledges an OPEN; wake any waiter for that unit.
			c.dispatchAck(m.Src)
		default:
			// IDENT / other control frames are currently informational.
		}
	}
}

func (c *Client) fail(err error) {
	c.errMu.Lock()
	if c.readErr == nil {
		c.readErr = err
	}
	c.errMu.Unlock()
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *Client) closedErr() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.readErr != nil && !errors.Is(c.readErr, io.EOF) {
		return c.readErr
	}
	return net.ErrClosed
}

// Close shuts down the client and its connection.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
	return nil
}

// readMessage reads exactly one framed message from r.
func readMessage(r *bufio.Reader) (Message, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Message{}, err
	}
	if hdr[0] != Magic[0] || hdr[1] != Magic[1] {
		return Message{}, ErrMagic
	}
	outer := int(binary.BigEndian.Uint16(hdr[2:4]))
	full := make([]byte, 4+outer)
	copy(full, hdr[:])
	if _, err := io.ReadFull(r, full[4:]); err != nil {
		return Message{}, err
	}
	m, _, err := Decode(full)
	return m, err
}
