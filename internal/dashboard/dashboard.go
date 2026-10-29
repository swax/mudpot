// Package dashboard serves a private, read-only view of the persistent session history.
package dashboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/swax/mudpot/internal/adventure"
	"github.com/swax/mudpot/internal/geoip"
	"github.com/swax/mudpot/internal/history"
)

type Config struct {
	Address, LogPath, HistoryPath, Token string
	GeoIP                                *geoip.Resolver
}

type pageView struct {
	*history.View
	Geo        map[string]geoip.Info
	GeoEnabled bool
}

func enrich(v *history.View, resolver *geoip.Resolver) (pageView, error) {
	result := pageView{View: v, Geo: map[string]geoip.Info{}, GeoEnabled: resolver != nil}
	if resolver == nil {
		return result, nil
	}
	add := func(ip string) error {
		if _, found := result.Geo[ip]; found {
			return nil
		}
		info, err := resolver.Lookup(ip)
		if err != nil {
			return err
		}
		result.Geo[ip] = info
		return nil
	}
	for _, h := range v.Hosts {
		if err := add(h.IP); err != nil {
			return result, err
		}
	}
	for _, s := range v.Sessions {
		if s.Progress > 0 {
			if err := add(s.IP); err != nil {
				return result, err
			}
		}
	}
	if v.Selected != nil && v.Selected.Progress > 0 {
		if err := add(v.Selected.IP); err != nil {
			return result, err
		}
	}
	return result, nil
}

// Listen rejects non-loopback IP literals. Remote access should use an SSH tunnel.
func Listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("dashboard must bind a loopback IP literal, e.g. 127.0.0.1:8080; use an SSH tunnel for remote access")
	}
	return net.Listen("tcp", address)
}
func Serve(ctx context.Context, cfg Config) error {
	l, err := Listen(cfg.Address)
	if err != nil {
		return err
	}
	s := &http.Server{Handler: Handler(cfg), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 8192}
	// Bound private dashboard connections too; loopback does not imply unlimited resources.
	limited := &limitedListener{Listener: l, slots: make(chan struct{}, 8)}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-done:
		}
	}()
	err = s.Serve(limited)
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

type limitedListener struct {
	net.Listener
	slots chan struct{}
}
type limitedConn struct {
	net.Conn
	release func()
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.release(); return err }
func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
			var once sync.Once
			return &limitedConn{Conn: c, release: func() { once.Do(func() { <-l.slots }) }}, nil
		default:
			c.Close()
		}
	}
}

func Handler(cfg Config) http.Handler {
	slots := make(chan struct{}, 1)
	var cacheMu sync.Mutex
	cache := map[string]cachedPage{}
	wanted := sha256.Sum256([]byte(cfg.Token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Host == "" || !loopbackHost(r.Host) {
			http.Error(w, "Invalid dashboard host", http.StatusForbidden)
			return
		}
		if cfg.Token != "" {
			// Basic auth supports a browser prompt; bearer auth supports private monitoring tools.
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if _, password, ok := r.BasicAuth(); ok {
				provided = password
			}
			got := sha256.Sum256([]byte(provided))
			if subtle.ConstantTimeCompare(wanted[:], got[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="MudPot monitor"`)
				http.Error(w, "Authentication required", http.StatusUnauthorized)
				return
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Read-only dashboard", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		query, err := parseQuery(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key := queryURL(query)
		cacheMu.Lock()
		cached, hit := cache[key]
		cacheMu.Unlock()
		if hit && time.Now().Before(cached.until) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Method != "HEAD" {
				w.Write(cached.body)
			}
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "Dashboard busy", http.StatusServiceUnavailable)
			return
		}
		historyPath := cfg.HistoryPath
		if historyPath == "" {
			historyPath = history.Path(cfg.LogPath)
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		view, err := history.Read(ctx, historyPath, query)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "Unable to read session history. Start the upgraded honeypot first and check history file permissions.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == "HEAD" {
			return
		}
		var output bytes.Buffer
		renderView, err := enrich(view, cfg.GeoIP)
		if err != nil {
			http.Error(w, "Unable to read local GeoIP databases", http.StatusServiceUnavailable)
			return
		}
		if err := page.Execute(&output, renderView); err != nil {
			http.Error(w, "Unable to render history", http.StatusInternalServerError)
			return
		}
		body := output.Bytes()
		if len(body) <= 512*1024 {
			cacheMu.Lock()
			if len(cache) >= 4 {
				for k := range cache {
					delete(cache, k)
					break
				}
			}
			cache[key] = cachedPage{body: body, until: time.Now().Add(5 * time.Second)}
			cacheMu.Unlock()
		}
		w.Write(body)
	})
}
func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

type cachedPage struct {
	body  []byte
	until time.Time
}

func parseQuery(values url.Values) (history.Query, error) {
	q := history.Query{View: values.Get("view"), Search: strings.TrimSpace(values.Get("q")), Activity: values.Get("activity"),
		Session: values.Get("session"), Host: values.Get("host"), Before: values.Get("before"), After: values.Get("after"), EventPage: 1}
	if value := values.Get("event_page"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			return q, history.ErrQuery
		}
		q.EventPage = n
	}
	return q, history.Validate(q)
}

// Relative links keep the existing /mudpot/ reverse proxy prefix.
func queryURL(q history.Query) string {
	v := url.Values{}
	for _, field := range []struct{ k, v string }{{"view", q.View}, {"q", q.Search}, {"activity", q.Activity}, {"host", q.Host}, {"session", q.Session}, {"before", q.Before}, {"after", q.After}} {
		if field.v != "" {
			v.Set(field.k, field.v)
		}
	}
	if q.Session != "" {
		v.Set("event_page", strconv.Itoa(max(q.EventPage, 1)))
	}
	return "?" + v.Encode()
}
func browseURL(q history.Query, before, after string) string {
	q.Before, q.After = before, after
	q.Session = ""
	return queryURL(q)
}
func sessionURL(q history.Query, id string) string {
	q.Session = id
	q.EventPage = 1
	return queryURL(q)
}
func eventsURL(q history.Query, page int) string { q.EventPage = page; return queryURL(q) }
func hostURL(q history.Query, ip string) string {
	q.View = "sessions"
	q.Host = ip
	q.Session = ""
	q.Before = ""
	q.After = ""
	return queryURL(q)
}
func visible(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r < 32 || (r >= 127 && r <= 159) {
			fmt.Fprintf(&b, "[%02X]", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

//go:embed dashboard.html
var pageHTML string

var page = template.Must(template.New("monitor").Funcs(template.FuncMap{
	"clock":   func(t time.Time) string { return t.Format("Jan 02 2006 15:04:05 UTC") },
	"visible": visible, "clean": adventure.Clean, "join": func(items []string) string { return strings.Join(items, ", ") }, "route": func(items []string) string { return strings.Join(items, " → ") },
	"browseURL": browseURL, "sessionURL": sessionURL, "eventsURL": eventsURL, "hostURL": hostURL, "prev": func(n int) int { return n - 1 }, "next": func(n int) int { return n + 1 },
}).Parse(pageHTML))
