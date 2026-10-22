package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swax/mudpot/internal/eventlog"
)

func TestPrivateBinding(t *testing.T) {
	for _, address := range []string{":8080", "0.0.0.0:0", "[::]:0", "localhost:8080", "example.com:8080"} {
		if l, err := Listen(address); err == nil {
			l.Close()
			t.Fatalf("accepted public/ambiguous bind %s", address)
		}
	}
	l, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
}
func TestDashboardAuthEscapingAndReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := eventlog.Open(path, 100000)
	if err != nil {
		t.Fatal(err)
	}
	base := eventlog.Event{Session: "test", IP: "203.0.113.1", Protocol: "ssh", Port: 2223, Room: "vault"}
	base.Kind = "connect"
	l.Emit(base)
	base.Kind = "command"
	base.Text = "<script>alert(1)</script>\x1b[31m"
	l.Emit(base)
	base.Kind = "victory"
	l.Emit(base)
	l.Close()
	h := Handler(Config{LogPath: path, Token: "dashboard-secret"})
	request := func(method, path, host, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1"+path, nil)
		r.Host = host
		if token != "" {
			r.SetBasicAuth("operator", token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/", "127.0.0.1:8080", ""); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/", "127.0.0.1:8080", "wrong"); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	w := request("GET", "/", "127.0.0.1:8080", "dashboard-secret")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "vault recovered") {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "&lt;script&gt;") || strings.Contains(w.Body.String(), "\x1b") {
		t.Fatal("unsafe dashboard content")
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing CSP")
	}
	if w := request("POST", "/", "127.0.0.1:8080", "dashboard-secret"); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/.env", "127.0.0.1:8080", "dashboard-secret"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request("GET", "/", "evil.example", "dashboard-secret"); w.Code != 403 {
		t.Fatal("DNS rebinding", w.Code)
	}
	v, err := Read(context.Background(), path)
	if err != nil || v.Total != 1 || v.Commands != 1 || v.Victories != 1 {
		t.Fatal(v, err)
	}
}
