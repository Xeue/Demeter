package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
	"github.com/Xeue/Demeter/internal/model"
	"github.com/Xeue/Demeter/internal/pool"
	"github.com/Xeue/Demeter/internal/scan"
	"github.com/Xeue/Demeter/internal/store"
	"github.com/coder/websocket"
)

// TestRepro_TenFramesTwoOffline reproduces the field report: 10 frames, #7 and #8
// unreachable. It drives the full stack with the MockDialer and reads the actual
// `frames` snapshot back off the WebSocket, asserting all 10 frames survive and
// the payload is valid JSON, to localise the missing-frames bug to backend vs UI.
func TestRepro_TenFramesTwoOffline(t *testing.T) {
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
	sc := &scan.Scanner{DB: db, Pool: pool.New(8), Events: h}
	dialer := &device.MockDialer{Cards: 6, Offline: map[string]bool{
		"10.0.0.7": true, "10.0.0.8": true,
	}}
	mgr := manager.New(ctx, sc, dialer, st, h, st.Frames(), st.Groups(), 300*time.Millisecond, manager.AutoRebootOptions{})
	h.SetEngine(mgr)
	go h.Run(ctx.Done())
	mgr.Start()

	// Add 10 frames numbered 1..10.
	for i := 1; i <= 10; i++ {
		mgr.AddFrame(fmt.Sprintf("10.0.0.%d", i), strconv.Itoa(i), fmt.Sprintf("Frame %d", i), "", "ucp")
	}

	srv, err := NewServer(cfg, "test", a, h)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Let scans run so the online frames populate their cards (large payload).
	time.Sleep(1500 * time.Millisecond)

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
	conn.SetReadLimit(64 << 20) // browsers have no limit; raise ours so a big frames msg is readable

	// Read the very first `frames` message the server pushes on connect (mirrors
	// what the browser's window.frames is set to), and measure it.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rctx, rc := context.WithTimeout(ctx, 2*time.Second)
		_, data, err := conn.Read(rctx)
		rc()
		if err != nil {
			t.Logf("read err (continuing): %v", err)
			continue
		}
		var env hub.Envelope
		if json.Unmarshal(data, &env) != nil || env.Command != "frames" {
			continue
		}
		var frames model.Frames
		if err := json.Unmarshal(env.Data, &frames); err != nil {
			t.Fatalf("frames payload is INVALID JSON (%d bytes): %v", len(data), err)
		}
		nums := []int{}
		online := 0
		params := 0
		for _, f := range frames {
			n, _ := strconv.Atoi(f.Number)
			nums = append(nums, n)
			if !f.Offline {
				online++
			}
			for _, sl := range f.Slots {
				params += len(sl.Active)
			}
		}
		sort.Ints(nums)
		t.Logf("FRAMES MESSAGE: %d bytes, %d frames, numbers=%v, online=%d, total active params=%d",
			len(data), len(frames), nums, online, params)
		if out := os.Getenv("DEMETER_DUMP_FRAMES"); out != "" {
			if err := os.WriteFile(out, env.Data, 0o644); err != nil {
				t.Fatalf("dump frames: %v", err)
			}
			t.Logf("dumped frames payload to %s", out)
		}
		if len(frames) != 10 {
			t.Fatalf("BUG REPRODUCED: server delivered %d frames, want 10 (numbers=%v)", len(frames), nums)
		}
		t.Logf("backend OK: all 10 frames delivered as valid JSON")
		return
	}
	t.Fatal("never received a frames message")
}
