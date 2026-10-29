package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swax/mudpot/internal/eventlog"
	"github.com/swax/mudpot/internal/geoip"
	"github.com/swax/mudpot/internal/history"
)

func TestOfflineGeoIPOnPlayerReports(t *testing.T) {
	r, err := geoip.Open(filepath.Join("..", "..", "data", "geoip"))
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Skip("local MaxMind databases are not installed")
	}
	defer r.Close()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := history.Open(history.Path(path))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Record([]eventlog.Event{{Time: time.Now(), Session: "player", IP: "8.8.8.8", Kind: "move", Room: "cargo"}, {Time: time.Now(), Session: "scanner", IP: "1.1.1.1", Kind: "command", Text: "whoami"}}); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{LogPath: path, GeoIP: r})
	for _, path := range []string{"/", "/?session=player", "/?view=recent"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1"+path, nil))
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, "United States (US)") || !strings.Contains(body, "AS15169") || !strings.Contains(body, "Google LLC") {
			t.Fatal("missing player enrichment", path, w.Code)
		}
		if strings.Contains(body, "Cloudflare") {
			t.Fatal("routine scanner was enriched")
		}
	}
}

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

func TestPlayersProgressBrowsingAndRetentionNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := history.Open(history.Path(path))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	var events []eventlog.Event
	for i := 0; i < 61; i++ {
		events = append(events, eventlog.Event{Time: now.Add(time.Duration(i) * time.Second), Session: fmt.Sprintf("visitor-%02d", i), IP: fmt.Sprintf("192.0.2.%d", i+1), Kind: "move", Room: "cargo", Text: "east"})
	}
	events = append(events, eventlog.Event{Time: now.Add(1000 * time.Second), Session: "scanner", IP: "198.51.100.99", Kind: "command", Text: "whoami"})
	for i := 0; i < 105; i++ {
		events = append(events, eventlog.Event{Time: now.Add(time.Duration(i+100) * time.Second), Session: "visitor-00", IP: "192.0.2.1", User: "A&B <captain>", Kind: "command", Text: "<script> marker"})
	}
	if err = store.Record(events); err != nil {
		t.Fatal(err)
	}
	h := Handler(Config{LogPath: path})
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1"+path, nil))
		return w
	}
	for _, test := range []struct{ path, want string }{
		{"/", "Game-playing hosts"}, {"/?view=sessions", "Gameplay sessions"}, {"/?q=captain", "Browse gameplay sessions"},
		{"/?q=missing", "No players match yet"}, {"/?view=recent", "198.51.100.99"}, {"/?view=stats", "Daily statistics"},
		{"/?session=visitor-00&q=captain", "Event page 1 of 2"}, {"/?session=visitor-00&event_page=2", "Previous events"},
	} {
		w := get(test.path)
		if w.Code != 200 || !strings.Contains(w.Body.String(), test.want) {
			t.Fatalf("%s: %d: expected %s", test.path, w.Code, test.want)
		}
		if strings.Contains(w.Body.String(), "<script>") {
			t.Fatal("unsafe output")
		}
	}
	if w := get("/"); strings.Contains(w.Body.String(), "198.51.100.99") {
		t.Fatal("scanner included in default report")
	}
	if w := get("/?session=visitor-00&q=captain"); !strings.Contains(w.Body.String(), "A&amp;B &lt;captain&gt;") || !strings.Contains(w.Body.String(), "q=captain&amp;session=visitor-00") {
		t.Fatal("escaping or filter lost")
	}
	if w := get("/"); !strings.Contains(w.Header().Get("Content-Security-Policy"), "form-action 'self'") {
		t.Fatal("form blocked by CSP")
	}
	for _, path := range []string{"/?before=bad", "/?event_page=0", "/?view=unknown", "/?activity=unknown", "/?q=" + strings.Repeat("a", 257)} {
		if w := get(path); w.Code != 400 {
			t.Fatal("invalid query accepted", path, w.Code)
		}
	}
	if w := get("/?session=missing"); w.Code != 404 {
		t.Fatal("missing session", w.Code)
	}
	v, err := history.Read(context.Background(), history.Path(path), history.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if w := get("/?before=" + v.Next); w.Code != 200 || !strings.Contains(w.Body.String(), "Newer") {
		t.Fatal("cursor navigation failed")
	}
	// Verify summary visibility after transcript expiry using a fresh handler (no cached response).
	for i := 0; i < 4; i++ {
		if _, err = store.Cleanup(context.Background(), now.Add(181*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	h = Handler(Config{LogPath: path})
	if w := get("/?session=visitor-00"); w.Code != 200 || !strings.Contains(w.Body.String(), "Event details have expired") {
		t.Fatal("expiry notice missing")
	}
	if w := get("/"); strings.Contains(w.Body.String(), `href="/?`) {
		t.Fatal("links break public proxy prefix")
	}
}

func TestCachedReportsRemainAuthenticated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := history.Open(history.Path(path))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := Handler(Config{LogPath: path, Token: "secret"})
	get := func(token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://127.0.0.1/", nil)
		if token != "" {
			r.SetBasicAuth("operator", token)
		}
		h.ServeHTTP(w, r)
		return w
	}
	first := get("secret")
	if first.Code != 200 {
		t.Fatal(first.Code)
	}
	if err = store.Record([]eventlog.Event{{Time: time.Now(), Session: "new-player", IP: "192.0.2.1", Kind: "move", Room: "cargo"}}); err != nil {
		t.Fatal(err)
	}
	if cached := get("secret"); cached.Body.String() != first.Body.String() {
		t.Fatal("identical requests did not use bounded response cache")
	}
	if get("").Code != 401 || get("wrong").Code != 401 {
		t.Fatal("cache bypassed authentication")
	}
}
func TestDashboardAuthEscapingAndReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := history.Open(history.Path(path))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	l, err := eventlog.OpenRecorded(path, 100000, store)
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
	w := request("GET", "/?session=test", "127.0.0.1:8080", "dashboard-secret")
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
	v, err := history.Read(context.Background(), history.Path(path), history.Query{})
	if err != nil || v.Total != 1 || v.Commands != 1 || v.Victories != 1 {
		t.Fatal(v, err)
	}
}
