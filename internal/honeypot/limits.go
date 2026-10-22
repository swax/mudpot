package honeypot

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var errBudget = errors.New("connection byte budget exceeded")

type admission struct {
	mu         sync.Mutex
	active     map[net.Conn]string
	ips        map[string]int
	max, perIP int
	closed     bool
}

func newAdmission(max, perIP int) *admission {
	return &admission{active: map[net.Conn]string{}, ips: map[string]int{}, max: max, perIP: perIP}
}
func (a *admission) acquire(c net.Conn) bool {
	ip, _, _ := net.SplitHostPort(c.RemoteAddr().String())
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || len(a.active) >= a.max || a.ips[ip] >= a.perIP {
		return false
	}
	a.active[c] = ip
	a.ips[ip]++
	return true
}
func (a *admission) release(c net.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ip, ok := a.active[c]; ok {
		delete(a.active, c)
		a.ips[ip]--
		if a.ips[ip] == 0 {
			delete(a.ips, ip)
		}
	}
}
func (a *admission) closeAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	for c := range a.active {
		c.Close()
	}
}

// guardedConn bounds handshakes, total lifetime, idle reads, slow writes, and wire bytes.
// phase is atomic because SSH reads and writes concurrently.
type guardedConn struct {
	net.Conn
	idle, writeTimeout    time.Duration
	end                   time.Time
	phase                 atomic.Int64
	readBytes, writeBytes atomic.Int64
	maxRead, maxWrite     int64
}

func guard(c net.Conn, cfg Config) *guardedConn {
	g := &guardedConn{Conn: c, idle: cfg.IdleTimeout, writeTimeout: 5 * time.Second, end: time.Now().Add(cfg.MaxLifetime), maxRead: 256 * 1024, maxWrite: 1024 * 1024}
	g.phase.Store(time.Now().Add(cfg.HandshakeTimeout).UnixNano())
	return g
}
func (g *guardedConn) deadline(d time.Duration) time.Time {
	t := time.Now().Add(d)
	if g.end.Before(t) {
		t = g.end
	}
	if n := g.phase.Load(); n != 0 && time.Unix(0, n).Before(t) {
		t = time.Unix(0, n)
	}
	return t
}
func (g *guardedConn) Read(p []byte) (int, error) {
	remaining := g.maxRead - g.readBytes.Load()
	if remaining <= 0 {
		return 0, errBudget
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	if err := g.Conn.SetReadDeadline(g.deadline(g.idle)); err != nil {
		return 0, err
	}
	n, err := g.Conn.Read(p)
	g.readBytes.Add(int64(n))
	return n, err
}
func (g *guardedConn) Write(p []byte) (int, error) {
	if g.writeBytes.Add(int64(len(p))) > g.maxWrite {
		return 0, errBudget
	}
	if err := g.Conn.SetWriteDeadline(g.deadline(g.writeTimeout)); err != nil {
		return 0, err
	}
	return g.Conn.Write(p)
}

type commandBudget struct {
	start        time.Time
	count, total int
	max          int
}

func (b *commandBudget) allow() bool {
	if time.Since(b.start) >= time.Minute {
		b.start = time.Now()
		b.count = 0
	}
	b.count++
	b.total++
	return b.count <= b.max && b.total <= 600
}
