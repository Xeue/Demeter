package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Xeue/Demeter/internal/auth"
	"github.com/Xeue/Demeter/internal/commandsdb"
	"github.com/Xeue/Demeter/internal/config"
	"github.com/Xeue/Demeter/internal/device"
	"github.com/Xeue/Demeter/internal/hub"
	"github.com/Xeue/Demeter/internal/manager"
	"github.com/Xeue/Demeter/internal/pool"
	"github.com/Xeue/Demeter/internal/scan"
	"github.com/Xeue/Demeter/internal/store"
	"github.com/coder/websocket"
)

// TestRepro_SlowClientLosesLaterFrames simulates a real browser that can't drain
// the WebSocket as fast as the scan loop fires per-slot updates: it reads slowly.
// With 10 latency-simulating frames it reproduces the field report — the later
// frames' slotInfo/frameStatus never arrive (and/or the client gets disconnected).
func TestRepro_SlowClientLosesLaterFrames(t *testing.T) {
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
	st, _ := store.New(dir)
	db, _ := commandsdb.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := hub.New(a)
	sc := &scan.Scanner{DB: db, Pool: pool.New(8), Events: h}
	dialer := &device.MockDialer{Cards: 6, Latency: 8 * time.Millisecond, Offline: map[string]bool{
		"10.0.0.7": true, "10.0.0.8": true,
	}}
	mgr := manager.New(ctx, sc, dialer, st, h, st.Frames(), st.Groups(), 500*time.Millisecond, manager.AutoRebootOptions{})
	h.SetEngine(mgr)
	go h.Run(ctx.Done())
	mgr.Start()
	for i := 1; i <= 10; i++ {
		mgr.AddFrame("10.0.0."+itoa(i), itoa(i), "Frame "+itoa(i), "", "ucp")
	}

	srv, _ := NewServer(cfg, "test", a, h)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ := client.PostForm(ts.URL+"/login", url.Values{"username": {"admin"}, "password": {"secret"}})
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
	conn.SetReadLimit(64 << 20)

	// Track which frames we ever receive a slotInfo (card data) for, and which we
	// receive an "offline" frameStatus for. Read SLOWLY to mimic a busy browser.
	slotSeen := map[string]bool{}
	offlineSeen := map[string]bool{}
	disconnects := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rctx, rc := context.WithTimeout(ctx, 2*time.Second)
		_, data, err := conn.Read(rctx)
		rc()
		if err != nil {
			disconnects++
			// reconnect like backend-shim does
			nc, _, derr := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws",
				&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
			if derr != nil {
				continue
			}
			nc.SetReadLimit(64 << 20)
			conn = nc
			continue
		}
		var env hub.Envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		switch env.Command {
		case "slotInfo":
			var m struct {
				Frame struct {
					IP string `json:"ip"`
				} `json:"frame"`
			}
			if json.Unmarshal(env.Data, &m) == nil {
				slotSeen[m.Frame.IP] = true
			}
		case "frameStatus":
			var m struct {
				FrameIP string `json:"frameIP"`
				Offline bool   `json:"offline"`
			}
			if json.Unmarshal(env.Data, &m) == nil && m.Offline {
				offlineSeen[m.FrameIP] = true
			}
		}
		time.Sleep(25 * time.Millisecond) // a functioning browser: keeps up once the firehose is deduped away
	}

	gotSlots := keys(slotSeen)
	gotOffline := keys(offlineSeen)
	t.Logf("client disconnects (slow-client drops): %d", disconnects)
	t.Logf("frames we ever got card data (slotInfo) for: %v", gotSlots)
	t.Logf("frames we ever got an offline status for:    %v (expect 10.0.0.7, 10.0.0.8)", gotOffline)
	// With server-side dedup the steady-state firehose is gone, so even a paced
	// client receives every online frame's cards and both offline statuses.
	if len(gotSlots) < 8 {
		t.Errorf("only %d/8 online frames delivered card data — later frames still starved", len(gotSlots))
	}
	if len(gotOffline) < 2 {
		t.Errorf("only %d/2 unreachable frames reported offline — offline status still lost", len(gotOffline))
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
