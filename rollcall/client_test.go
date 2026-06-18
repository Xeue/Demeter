package rollcall

import (
	"bufio"
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeFrame answers GET/SET on an in-memory connection, swapping addresses and
// echoing a canned value - enough to exercise framing + reply routing with no
// real hardware.
func fakeFrame(t *testing.T, conn net.Conn, answer func(req Message) Message) {
	t.Helper()
	go func() {
		defer conn.Close()
		br := bufio.NewReader(conn)
		for {
			req, err := readMessage(br)
			if err != nil {
				return
			}
			reply := answer(req)
			if _, err := conn.Write(reply.Encode()); err != nil {
				return
			}
		}
	}()
}

func TestClientGet(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	fakeFrame(t, srvConn, func(req Message) Message {
		return Message{Dst: req.Src, Src: req.Dst, Opcode: OpReply, CmdID: req.CmdID, Value: Str("10.100.44.12")}
	})
	c := NewConn(cliConn, WithSelf(client))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	v, err := c.Get(ctx, unit, 4101)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v.Kind != KindString || v.Str != "10.100.44.12" {
		t.Errorf("got %s, want str(10.100.44.12)", v)
	}
}

func TestClientSetEchoesValue(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	fakeFrame(t, srvConn, func(req Message) Message {
		// A real device echoes the resulting value in its reply.
		return Message{Dst: req.Src, Src: req.Dst, Opcode: OpReply, CmdID: req.CmdID, Value: req.Value}
	})
	c := NewConn(cliConn, WithSelf(client))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	v, err := c.Set(ctx, unit, 4108, Int(1))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v.Kind != KindInt || v.Int != 1 {
		t.Errorf("got %s, want int(1)", v)
	}
}

// Concurrent gets for different command ids must each get their own reply.
func TestClientConcurrentGets(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	fakeFrame(t, srvConn, func(req Message) Message {
		return Message{Dst: req.Src, Src: req.Dst, Opcode: OpReply, CmdID: req.CmdID, Value: Int(req.CmdID)}
	})
	c := NewConn(cliConn, WithSelf(client))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmds := []uint32{4100, 4101, 4103, 4108, 48729}
	type res struct {
		cmd uint32
		v   Value
		err error
	}
	results := make(chan res, len(cmds))
	for _, cmd := range cmds {
		go func(cmd uint32) {
			v, err := c.Get(ctx, unit, cmd)
			results <- res{cmd, v, err}
		}(cmd)
	}
	for range cmds {
		r := <-results
		if r.err != nil {
			t.Errorf("cmd %d: %v", r.cmd, r.err)
			continue
		}
		if r.v.Kind != KindInt || r.v.Int != r.cmd {
			t.Errorf("cmd %d: got %s, want int(%d)", r.cmd, r.v, r.cmd)
		}
	}
}

func TestClientUnsolicitedNotify(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	c := NewConn(cliConn, WithSelf(client))
	defer c.Close()

	// Push an unsolicited update with nobody waiting on it.
	go func() {
		n := Message{Dst: client, Src: unit, Opcode: OpReply, Flags: FlagNotify, CmdID: 37139, Value: Str("   +0.0uS")}
		srvConn.Write(n.Encode())
	}()

	select {
	case m := <-c.Notify():
		if m.CmdID != 37139 || m.Value.Str != "   +0.0uS" {
			t.Errorf("unexpected notify: cmd=%d %s", m.CmdID, m.Value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for notify")
	}
}

func TestClientOpen(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	go func() {
		defer srvConn.Close()
		br := bufio.NewReader(srvConn)
		for {
			req, err := readMessage(br)
			if err != nil {
				return
			}
			if req.Opcode == OpOpen {
				ack := Message{Dst: req.Src, Src: req.Dst, Opcode: OpAck}
				srvConn.Write(ack.Encode())
			}
		}
	}()
	c := NewConn(cliConn, WithSelf(client))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Open(ctx, unit); err != nil {
		t.Fatalf("Open: %v", err)
	}
}

func TestClientBatchGet(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	fakeFrame(t, srvConn, func(req Message) Message {
		return Message{Dst: req.Src, Src: req.Dst, Opcode: OpReply, CmdID: req.CmdID, Value: Int(req.CmdID)}
	})
	c := NewConn(cliConn, WithSelf(client))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmds := []uint32{4100, 4101, 4103, 4105, 4108, 48729}
	vals, errs := c.BatchGet(ctx, unit, cmds)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	for _, cmd := range cmds {
		if v, ok := vals[cmd]; !ok || v.Int != cmd {
			t.Errorf("cmd %d: got %v ok=%v", cmd, v, ok)
		}
	}
}

// With WithMaxInFlight(2), no more than 2 requests may be outstanding at once.
func TestMaxInFlightBound(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	var mu sync.Mutex
	inflight, maxSeen := 0, 0
	go func() {
		defer srvConn.Close()
		br := bufio.NewReader(srvConn)
		var wmu sync.Mutex
		for {
			req, err := readMessage(br)
			if err != nil {
				return
			}
			mu.Lock()
			inflight++
			if inflight > maxSeen {
				maxSeen = inflight
			}
			mu.Unlock()
			go func(req Message) {
				time.Sleep(20 * time.Millisecond)
				// Mark processing done BEFORE writing the reply: the reply is in
				// transit until the client reads it (when it frees its in-flight
				// slot), so decrementing after the write races with the client
				// issuing its next request and overcounts.
				mu.Lock()
				inflight--
				mu.Unlock()
				reply := Message{Dst: req.Src, Src: req.Dst, Opcode: OpReply, CmdID: req.CmdID, Value: Int(req.CmdID)}
				wmu.Lock()
				srvConn.Write(reply.Encode())
				wmu.Unlock()
			}(req)
		}
	}()
	c := NewConn(cliConn, WithSelf(client), WithMaxInFlight(2))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmds := []uint32{1, 2, 3, 4, 5, 6, 7, 8}
	vals, errs := c.BatchGet(ctx, unit, cmds)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(vals) != len(cmds) {
		t.Fatalf("got %d values, want %d", len(vals), len(cmds))
	}
	mu.Lock()
	ms := maxSeen
	mu.Unlock()
	if ms > 2 {
		t.Errorf("max in-flight reached %d, want <= 2", ms)
	}
}

func TestClientGetContextCancel(t *testing.T) {
	cliConn, srvConn := net.Pipe()
	// Server that never replies.
	go func() {
		br := bufio.NewReader(srvConn)
		readMessage(br) //nolint
		// drop on the floor
	}()
	c := NewConn(cliConn, WithSelf(client))
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, unit, 4101); err != context.DeadlineExceeded {
		t.Errorf("want DeadlineExceeded, got %v", err)
	}
}
