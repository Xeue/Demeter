package hub

import (
	"context"
	"encoding/json"
	"net"

	"github.com/Xeue/Demeter/internal/auth"
	"github.com/coder/websocket"
)

// isLoopbackIP reports whether ip is a loopback address (the desktop window
// always connects over loopback).
func isLoopbackIP(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// Client is one connected browser, with its own bounded write queue.
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	session  *auth.Session
	remoteIP string
	send     chan []byte
	ctx      context.Context
	cancel   context.CancelFunc
}

func (c *Client) username() string {
	if c.session == nil {
		return "?"
	}
	return c.session.Username
}

func (c *Client) role() auth.Role {
	if c.session == nil {
		return ""
	}
	return c.session.Role
}

// requireRole reports whether the client meets the required role.
func (c *Client) requireRole(r auth.Role) bool {
	return c.session != nil && c.session.Role.AtLeast(r)
}

// audit records a destructive/auth action for this client.
func (c *Client) audit(action string, target any) {
	if c.session != nil {
		c.hub.auth.Audit().Log(c.session.Username, c.session.Role, action, target, c.remoteIP)
	}
}

// Serve registers a client (already-upgraded conn + validated session), pushes
// the current state, and runs its read/write pumps until disconnect. Blocks.
func (h *Hub) Serve(ctx context.Context, conn *websocket.Conn, session *auth.Session, remoteIP string) {
	cctx, cancel := context.WithCancel(ctx)
	c := &Client{
		hub:      h,
		conn:     conn,
		session:  session,
		remoteIP: remoteIP,
		send:     make(chan []byte, clientSendBuffer),
		ctx:      cctx,
		cancel:   cancel,
	}
	h.register <- c

	// Push current state so a (re)connecting client is immediately in sync.
	if h.engine != nil {
		c.trySend(encode(chFrames, h.engine.FramesSnapshot()))
		c.trySend(encode(chGroups, h.engine.GroupsSnapshot()))
	}
	// Replay recent log history (one batch) so the Logs page isn't empty on open.
	if logs := h.recentLogs(); len(logs) > 0 {
		c.trySend(encode(chLogs, logs))
	}

	// On first run, show the generated admin credentials, but ONLY to an
	// authenticated admin connecting over loopback (the desktop window), so the
	// plaintext password is never sent to a remote client.
	if c.requireRole(auth.RoleAdmin) && isLoopbackIP(remoteIP) {
		if n := h.auth.Notice(); n != nil {
			c.trySend(encode(chCredentials, n))
		}
	}

	go c.writePump()
	c.readPump()

	cancel()
	h.unregister <- c
	conn.CloseNow()
}

func (c *Client) trySend(msg []byte) {
	select {
	case c.send <- msg:
	default:
	}
}

func (c *Client) readPump() {
	for {
		_, data, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		c.hub.router.dispatch(c, env)
	}
}

func (c *Client) writePump() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.Write(c.ctx, websocket.MessageText, msg); err != nil {
				c.cancel()
				return
			}
		}
	}
}
