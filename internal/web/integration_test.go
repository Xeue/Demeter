package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/config"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/hub"
	"github.com/Xeue/Demeter/internal/manager"
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
	"github.com/Xeue/Demeter/internal/scan"
	"github.com/Xeue/Demeter/internal/store"
	"github.com/coder/websocket"
)

// TestWSEndToEnd exercises login -> authenticated WS upgrade -> inbound command
// routed to the manager -> outbound broadcast, across the full web/hub/auth/
// manager stack with a fake device dialer.
func TestWSEndToEnd(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir

	a, err := auth.New(dir, auth.NewAudit(dir, time.Now), false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateUser("admin", "secret", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := hub.New(a)
	sc := &scan.Scanner{DB: db, Pool: pool.New(4), Events: h}
	mgr := manager.New(ctx, sc, device.NewFakeDialer(), st, h, st.Frames(), st.Groups(), time.Hour, manager.AutoRebootOptions{})
	h.SetEngine(mgr)
	go h.Run(ctx.Done())
	mgr.Start()

	srv, err := NewServer(cfg, "test", a, h)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Log in (no redirect-following client) to grab the session cookie.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == "demeter_session" {
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie returned from login")
	}

	// Connect the WebSocket with the cookie.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	dialCtx, dialCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dialCancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {cookie}},
	})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.CloseNow()

	// Send addGroup and expect a "groups" broadcast containing it.
	send := hub.Envelope{Command: "addGroup", Data: json.RawMessage(`{"name":"g1","enabled":true}`)}
	b, _ := json.Marshal(send)
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("ws write: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, rc := context.WithTimeout(ctx, time.Second)
		_, data, err := conn.Read(readCtx)
		rc()
		if err != nil {
			continue
		}
		var env hub.Envelope
		if json.Unmarshal(data, &env) != nil || env.Command != "groups" {
			continue
		}
		var groups model.Groups
		if json.Unmarshal(env.Data, &groups) == nil {
			if _, ok := groups["g1"]; ok {
				return // success: round-tripped through auth+ws+router+manager+broadcast
			}
		}
	}
	t.Fatal("did not receive a groups broadcast containing g1")
}

// TestCredentialsNoticeOnLoopback confirms the first-run generated credentials
// are pushed to an authenticated admin connecting over loopback.
func TestCredentialsNoticeOnLoopback(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir

	a, err := auth.New(dir, auth.NewAudit(dir, time.Now), false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateUser("admin", "secret", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	a.SetNotice("admin", "generated-pw-123")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(a)
	go h.Run(ctx.Done())

	srv, err := NewServer(cfg, "test", a, h)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == "demeter_session" {
			cookie = c.Name + "=" + c.Value
		}
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dialCancel()
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.CloseNow()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, rc := context.WithTimeout(ctx, time.Second)
		_, data, err := conn.Read(readCtx)
		rc()
		if err != nil {
			continue
		}
		var env hub.Envelope
		if json.Unmarshal(data, &env) != nil || env.Command != "credentials" {
			continue
		}
		var n auth.Notice
		if json.Unmarshal(env.Data, &n) == nil && n.Username == "admin" && n.Password == "generated-pw-123" {
			return // got the notice
		}
	}
	t.Fatal("did not receive credentials notice over loopback WS")
}

// TestStageCardOverWS exercises the full pre-config path: add an (offline) frame,
// stage a card on a slot, and confirm it shows up in the frames snapshot as a
// staged slot — i.e. router -> manager -> actor -> state, end to end.
func TestStageCardOverWS(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir

	a, err := auth.New(dir, auth.NewAudit(dir, time.Now), false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateUser("admin", "secret", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(a)
	sc := &scan.Scanner{DB: db, Pool: pool.New(4), Events: h}
	mgr := manager.New(ctx, sc, device.NewFakeDialer(), st, h, st.Frames(), st.Groups(), time.Hour, manager.AutoRebootOptions{})
	h.SetEngine(mgr)
	go h.Run(ctx.Done())
	mgr.Start()

	srv, err := NewServer(cfg, "test", a, h)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"secret"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == "demeter_session" {
			cookie = c.Name + "=" + c.Value
		}
	}

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer conn.CloseNow()

	write := func(cmd string, data string) {
		b, _ := json.Marshal(hub.Envelope{Command: cmd, Data: json.RawMessage(data)})
		if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatalf("ws write %s: %v", cmd, err)
		}
	}
	write("addFrame", `{"ip":"10.9.9.9","number":"9","name":"staged","group":""}`)
	write("stageCard", `{"ip":"10.9.9.9","slot":"05"}`)

	// One long-lived read context (a per-read timeout would close the socket).
	// Each loop sends getFrames so a reply is always pending and Read never
	// blocks long; we stop once the snapshot shows the staged slot.
	readCtx, rc := context.WithTimeout(ctx, 5*time.Second)
	defer rc()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		write("getFrames", `null`)
		_, data, err := conn.Read(readCtx)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}
		var env hub.Envelope
		if json.Unmarshal(data, &env) != nil || env.Command != "frames" {
			continue
		}
		var frames model.Frames
		if json.Unmarshal(env.Data, &frames) != nil {
			continue
		}
		if f, ok := frames["10.9.9.9"]; ok {
			if sl, ok := f.Slots["05"]; ok && sl.Staged {
				return // staged slot present end to end
			}
		}
	}
	t.Fatal("staged slot 05 never appeared in the frames snapshot")
}

// --- helpers for the import/export round-trip ---

func ioLogin(t *testing.T, ts *httptest.Server, user, pass string) string {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(ts.URL+"/login", url.Values{"username": {user}, "password": {pass}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "demeter_session" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func ioDial(t *testing.T, ctx context.Context, ts *httptest.Server, cookie string) *websocket.Conn {
	t.Helper()
	dctx, dcancel := context.WithTimeout(ctx, 3*time.Second)
	defer dcancel()
	conn, _, err := websocket.Dial(dctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws",
		&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	return conn
}

func ioSend(t *testing.T, ctx context.Context, conn *websocket.Conn, command string, data any) {
	t.Helper()
	raw, _ := json.Marshal(data)
	b, _ := json.Marshal(hub.Envelope{Command: command, Data: raw})
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("ws write %s: %v", command, err)
	}
}

func ioReadUntil(t *testing.T, ctx context.Context, conn *websocket.Conn, command string) json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rctx, rc := context.WithTimeout(ctx, time.Second)
		_, data, err := conn.Read(rctx)
		rc()
		if err != nil {
			continue
		}
		var env hub.Envelope
		if json.Unmarshal(data, &env) == nil && env.Command == command {
			return env.Data
		}
	}
	t.Fatalf("did not receive %q in time", command)
	return nil
}

// TestImportExportRoundTrip exercises getExport (authoritative snapshot) and a
// granular merge importData over the full WS stack: a new frame+group are merged
// in alongside the existing ones (not replacing them).
func TestImportExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	a, err := auth.New(dir, auth.NewAudit(dir, time.Now), false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CreateUser("admin", "secret", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	db, err := commandsdb.Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(a)
	sc := &scan.Scanner{DB: db, Pool: pool.New(4), Events: h}
	mgr := manager.New(ctx, sc, device.NewFakeDialer(), st, h, st.Frames(), st.Groups(), time.Hour, manager.AutoRebootOptions{})
	h.SetEngine(mgr)
	go h.Run(ctx.Done())
	mgr.Start()
	srv, err := NewServer(cfg, "test", a, h)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	conn := ioDial(t, ctx, ts, ioLogin(t, ts, "admin", "secret"))
	defer conn.CloseNow()

	ioSend(t, ctx, conn, "addGroup", map[string]any{"name": "g1", "enabled": true})
	ioSend(t, ctx, conn, "addFrame", map[string]any{"ip": "10.0.0.1", "number": "7", "group": "g1"})

	// Export must contain the frame and group.
	ioSend(t, ctx, conn, "getExport", nil)
	var ex struct {
		Frames model.Frames
		Groups model.Groups
	}
	_ = json.Unmarshal(ioReadUntil(t, ctx, conn, "exportData"), &ex)
	if ex.Frames["10.0.0.1"] == nil {
		t.Error("export snapshot missing the frame")
	}
	if ex.Groups["g1"] == nil {
		t.Error("export snapshot missing the group")
	}

	// Granular import of a NEW frame + group must merge (not replace).
	ioSend(t, ctx, conn, "importData", map[string]any{
		"frames": map[string]any{"10.0.0.2": map[string]any{"ip": "10.0.0.2", "number": "2", "scan": false, "slots": map[string]any{}}},
		"groups": map[string]any{"g2": map[string]any{"name": "g2"}},
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ioSend(t, ctx, conn, "getFrames", nil)
		var fr model.Frames
		_ = json.Unmarshal(ioReadUntil(t, ctx, conn, "frames"), &fr)
		if fr["10.0.0.1"] != nil && fr["10.0.0.2"] != nil {
			return // both present -> merge worked
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("imported frame did not appear alongside the existing one")
}

// TestUnauthedWSRejected confirms the WS upgrade is gated by auth.
func TestUnauthedWSRejected(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	a, _ := auth.New(dir, auth.NewAudit(dir, time.Now), false, time.Now)
	h := hub.New(a)
	srv, err := NewServer(cfg, "test", a, h)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err = websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", nil)
	if err == nil {
		t.Fatal("expected unauthenticated WS dial to be rejected")
	}
}
