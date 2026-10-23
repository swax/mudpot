// Package dashboard serves a private, read-only view of the honeypot's bounded event logs.
package dashboard

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/swax/mudpot/internal/adventure"
	"github.com/swax/mudpot/internal/eventlog"
)

type Config struct{ Address, LogPath, Token string }
type Session struct {
	ID, IP, Protocol, Room, Status, User string
	Port, Commands                       int
	Start, End                           time.Time
	Events                               []eventlog.Event
}
type Count struct {
	Name  string
	Value int
}
type View struct {
	Sessions                                             []*Session
	Total, Unique, Commands, Victories, Limited, Invalid int
	Rooms, TopCommands                                   []Count
	Sampled                                              bool
	Updated                                              time.Time
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
	slots := make(chan struct{}, 2)
	wanted := sha256.Sum256([]byte(cfg.Token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
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
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "Dashboard busy", http.StatusServiceUnavailable)
			return
		}
		view, err := Read(r.Context(), cfg.LogPath)
		if err != nil {
			http.Error(w, "Unable to read event logs; check dashboard log permissions.", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == "HEAD" {
			return
		}
		page.Execute(w, view)
	})
}
func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}
func Read(ctx context.Context, path string) (*View, error) {
	v := &View{Updated: time.Now().UTC()}
	sessions := map[string]*Session{}
	order := make([]string, 0, 2000)
	next := 0
	ips := map[string]bool{}
	rooms := map[string]int{}
	commands := map[string]int{}
	for _, name := range []string{path + ".1", path} {
		f, err := os.Open(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			f.Close()
			return nil, fmt.Errorf("invalid log file")
		}
		// Tail at most 2 MiB per file and discard a partial first record.
		const tailBytes = 2 * 1024 * 1024
		start := max(int64(0), info.Size()-tailBytes)
		if _, err = f.Seek(start, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
		r := bufio.NewReader(io.LimitReader(f, tailBytes))
		if start > 0 {
			r.ReadString('\n')
			v.Sampled = true
		}
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 4096), 16384)
		for scanner.Scan() {
			if ctx.Err() != nil {
				f.Close()
				return nil, ctx.Err()
			}
			var e eventlog.Event
			if json.Unmarshal(scanner.Bytes(), &e) != nil {
				v.Invalid++
				continue
			}
			if e.Session == "" {
				continue
			}
			entry := sessions[e.Session]
			if entry == nil {
				if len(sessions) >= 2000 {
					v.Sampled = true
					delete(sessions, order[next])
					order[next] = e.Session
					next = (next + 1) % 2000
				} else {
					order = append(order, e.Session)
				}
				entry = &Session{ID: e.Session, IP: e.IP, Protocol: e.Protocol, Port: e.Port, Start: e.Time, Status: "observed"}
				sessions[e.Session] = entry
			}
			entry.End = e.Time
			if e.Room != "" {
				entry.Room = e.Room
			}
			if e.User != "" {
				entry.User = adventure.Clean(e.User)
			}
			ips[e.IP] = true
			e.Text = visible(e.Text)
			if len(entry.Events) >= 40 {
				copy(entry.Events, entry.Events[1:])
				entry.Events = entry.Events[:39]
			}
			entry.Events = append(entry.Events, e)
			switch e.Kind {
			case "connect":
				entry.Status = "connected"
			case "disconnect":
				if entry.Status == "connected" {
					entry.Status = "disconnected"
				}
			case "command":
				entry.Commands++
				v.Commands++
				if len(commands) < 1000 || commands[e.Text] > 0 {
					commands[e.Text]++
				} else {
					v.Sampled = true
				}
			case "move":
				rooms[e.Room]++
			case "victory":
				v.Victories++
				entry.Status = "vault recovered"
			case "limit":
				v.Limited++
				entry.Status = "limited"
			case "handshake_error":
				entry.Status = "handshake failed"
			}
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, s := range sessions {
		v.Sessions = append(v.Sessions, s)
	}
	sort.Slice(v.Sessions, func(i, j int) bool { return v.Sessions[i].End.After(v.Sessions[j].End) })
	v.Total = len(v.Sessions)
	v.Unique = len(ips)
	if len(v.Sessions) > 50 {
		v.Sessions = v.Sessions[:50]
	}
	v.Rooms = counts(rooms, 256)
	v.TopCommands = counts(commands, 15)
	return v, nil
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
func counts(m map[string]int, limit int) []Count {
	var c []Count
	for name, value := range m {
		c = append(c, Count{name, value})
	}
	sort.Slice(c, func(i, j int) bool {
		if c[i].Value == c[j].Value {
			return c[i].Name < c[j].Name
		}
		return c[i].Value > c[j].Value
	})
	if len(c) > limit {
		c = c[:limit]
	}
	return c
}

var page = template.Must(template.New("monitor").Funcs(template.FuncMap{"clock": func(t time.Time) string { return t.Format("Jan 02 15:04:05 UTC") }}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>MudPot // Activity Monitor</title>
<style>
:root{color-scheme:dark;--bg:#0a1017;--panel:#111c27;--line:#26394a;--muted:#94aabd;--ink:#e5eff6;--green:#78e2ba}
*{box-sizing:border-box}body{background:var(--bg);color:var(--ink);font:14px/1.6 ui-monospace,Consolas,monospace;margin:0}main{max-width:1280px;margin:auto;padding:36px 24px}header{display:flex;justify-content:space-between;gap:24px;border-bottom:1px solid var(--line);padding-bottom:24px}.eyebrow{color:var(--green);font-size:12px;letter-spacing:.13em}h1{font-size:28px;margin:8px 0}h2{font-size:17px;margin:0 0 16px}p{color:var(--muted);margin:5px 0}.tag{display:inline-block;color:var(--green);border:1px solid #36664e;border-radius:5px;padding:4px 9px;align-self:start}.stats{display:grid;grid-template-columns:repeat(5,1fr);gap:12px;margin:24px 0}.stat,.panel{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:20px}.value{font-size:30px;color:var(--green)}.label{color:var(--muted);font-size:12px}.columns{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:24px}.row{display:flex;justify-content:space-between;gap:12px;border-bottom:1px solid var(--line);padding:7px 0;overflow-wrap:anywhere}.sessions{padding:0}.session{border-bottom:1px solid var(--line);padding:16px 20px}.summary{display:flex;flex-wrap:wrap;gap:12px;cursor:pointer}.ip{color:var(--green)}.muted{color:var(--muted)}.status{margin-left:auto;color:#e8c88e}.events{background:#09131c;padding:12px;margin-top:12px;border-radius:5px;max-height:350px;overflow:auto}.event{display:grid;grid-template-columns:170px 120px 1fr;gap:10px;padding:4px 0;overflow-wrap:anywhere}.empty{padding:30px;color:var(--muted)}footer{color:var(--muted);padding:24px 0}.notice{border-left:3px solid #e8c88e;padding:10px 15px;margin-bottom:24px;color:#e8c88e}@media(max-width:760px){main{padding:20px 12px}.stats{grid-template-columns:repeat(2,1fr)}.columns{grid-template-columns:1fr}header{display:block}.event{grid-template-columns:1fr}.status{margin-left:0}h1{font-size:23px}}
summary{list-style:none}summary::-webkit-details-marker{display:none}.summary::before{content:'▸';color:var(--muted)}details[open] .summary::before{content:'▾'}
</style></head><body><main><header><div><div class="eyebrow">GREY SECTOR / OPERATIONS</div><h1>MudPot station monitor</h1><p>Connection history, adventure progress, and recovery activity.</p></div><span class="tag">READ ONLY</span></header>
<section class="stats" aria-label="Overview"><div class="stat"><div class="value">{{.Total}}</div><div class="label">Observed sessions</div></div><div class="stat"><div class="value">{{.Unique}}</div><div class="label">Unique addresses</div></div><div class="stat"><div class="value">{{.Commands}}</div><div class="label">Commands</div></div><div class="stat"><div class="value">{{.Victories}}</div><div class="label">Vault recoveries</div></div><div class="stat"><div class="value">{{.Limited}}</div><div class="label">Resource limits</div></div></section>
{{if .Sampled}}<p class="notice">Showing a bounded sample from the latest logs. Counts describe this sample, not lifetime totals.</p>{{end}}{{if .Invalid}}<p class="notice">{{.Invalid}} incomplete or invalid log records skipped.</p>{{end}}
<div class="columns"><section class="panel"><h2>Room traffic</h2>{{range .Rooms}}<div class="row"><span>{{.Name}}</span><span>{{.Value}}</span></div>{{else}}<p>No room transitions yet.</p>{{end}}</section><section class="panel"><h2>Top commands</h2>{{range .TopCommands}}<div class="row"><span>{{.Name}}</span><span>{{.Value}}</span></div>{{else}}<p>No commands yet.</p>{{end}}</section></div>
<h2>Recent sessions</h2><section class="panel sessions">{{range .Sessions}}<details class="session"><summary><span class="summary"><span class="ip">{{.IP}}</span><span>{{.Protocol}} :{{.Port}}</span><span class="muted">{{.Room}} · {{.Commands}} commands</span><span class="status">{{.Status}}</span></span></summary><p>{{clock .Start}} → {{clock .End}}{{if .User}} · user {{.User}}{{end}}</p><p class="muted">{{.ID}}</p><div class="events">{{range .Events}}<div class="event"><span class="muted">{{clock .Time}}</span><span>{{.Kind}}</span><span>{{.Text}}</span></div>{{end}}</div></details>{{else}}<p class="empty">Awaiting first contact. Start the honeypot and connect to its recovery terminal.</p>{{end}}</section>
<footer>Updated {{clock .Updated}} · Last 50 sessions, up to 40 events each · Reload to refresh</footer></main></body></html>`))
