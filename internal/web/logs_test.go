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
	"github.com/Xeue/Demeter/internal/config"
	"github.com/Xeue/Demeter/internal/hub"
	"github.com/Xeue/Demeter/internal/logging"
	"github.com/coder/websocket"
)

// TestLogHistoryReplayedOnConnect: a log buffered before a client connects is
// replayed to it as a "logs" batch — so the Logs page is populated on open
// rather than empty.
func TestLogHistoryReplayedOnConnect(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := hub.New(a)
	go h.Run(ctx.Done())
	h.Log(logging.Event{TimeString: "10:00:00", Level: "I", Category: "SERVER", Message: "hello-log-xyz"})

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

	readCtx, rc := context.WithTimeout(ctx, 4*time.Second)
	defer rc()
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			t.Fatalf("never received the logs batch: %v", err)
		}
		var env hub.Envelope
		if json.Unmarshal(data, &env) != nil || env.Command != "logs" {
			continue
		}
		var logs []logging.Event
		if json.Unmarshal(env.Data, &logs) != nil {
			continue
		}
		for _, e := range logs {
			if e.Message == "hello-log-xyz" {
				return // history replayed over the real WS
			}
		}
	}
}
