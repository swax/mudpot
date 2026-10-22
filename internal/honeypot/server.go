package honeypot

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/swax/mudpot/internal/adventure"
	"github.com/swax/mudpot/internal/eventlog"
	"golang.org/x/crypto/ssh"
)

type Config struct {
	TCP, TLS, SSH                              []string
	StateDir                                   string
	MaxConnections, PerIP, CommandsPerMinute   int
	HandshakeTimeout, IdleTimeout, MaxLifetime time.Duration
	CapturePasswords                           bool
}

func Defaults() Config {
	return Config{TCP: []string{":2222"}, SSH: []string{":2223"}, StateDir: "data/state", MaxConnections: 20, PerIP: 4, CommandsPerMinute: 60, HandshakeTimeout: 5 * time.Second, IdleTimeout: time.Minute, MaxLifetime: 10 * time.Minute}
}
func (c Config) Validate() error {
	if len(c.TCP)+len(c.TLS)+len(c.SSH) == 0 {
		return errors.New("enable at least one listener")
	}
	if c.MaxConnections < 1 || c.MaxConnections > 1024 || c.PerIP < 1 || c.PerIP > c.MaxConnections || c.CommandsPerMinute < 1 || c.CommandsPerMinute > 600 {
		return errors.New("invalid connection or command limits")
	}
	if c.HandshakeTimeout <= 0 || c.IdleTimeout <= 0 || c.MaxLifetime < c.IdleTimeout {
		return errors.New("invalid timeouts")
	}
	seen := map[string]bool{}
	for _, addresses := range [][]string{c.TCP, c.TLS, c.SSH} {
		for _, address := range addresses {
			if _, _, err := net.SplitHostPort(address); err != nil {
				return fmt.Errorf("invalid listen address %q: %w", address, err)
			}
			if seen[address] {
				return fmt.Errorf("duplicate listener %s", address)
			}
			seen[address] = true
		}
	}
	return nil
}

type endpoint struct {
	listener net.Listener
	protocol string
}
type Server struct {
	cfg       Config
	world     *adventure.World
	log       *eventlog.Logger
	admission *admission
	endpoints []endpoint
	sshConfig *ssh.ServerConfig
	tlsConfig *tls.Config
	wg        sync.WaitGroup
	closeOnce sync.Once
	closing   chan struct{}
}

// New binds all listeners before accepting traffic. A partial startup closes every listener.
func New(cfg Config, w *adventure.World, logger *eventlog.Logger) (_ *Server, err error) {
	if err = cfg.Validate(); err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, world: w, log: logger, admission: newAdmission(cfg.MaxConnections, cfg.PerIP), closing: make(chan struct{})}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	if len(cfg.SSH)+len(cfg.TLS) > 0 {
		if err = stateDir(cfg.StateDir); err != nil {
			return nil, err
		}
	}
	if len(cfg.SSH) > 0 {
		key, keyErr := hostKey(cfg.StateDir)
		if keyErr != nil {
			return nil, keyErr
		}
		s.sshConfig = &ssh.ServerConfig{MaxAuthTries: 3, ServerVersion: "SSH-2.0-GreySector"}
		s.sshConfig.AddHostKey(key)
	}
	if len(cfg.TLS) > 0 {
		cert, certErr := tlsCertificate(cfg.StateDir)
		if certErr != nil {
			return nil, certErr
		}
		s.tlsConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	for _, group := range []struct {
		protocol  string
		addresses []string
	}{{"tcp", cfg.TCP}, {"tls", cfg.TLS}, {"ssh", cfg.SSH}} {
		for _, address := range group.addresses {
			listener, listenErr := net.Listen("tcp", address)
			if listenErr != nil {
				return nil, listenErr
			}
			s.endpoints = append(s.endpoints, endpoint{listener, group.protocol})
		}
	}
	return s, nil
}
func (s *Server) Addresses() map[string][]string {
	m := map[string][]string{}
	for _, ep := range s.endpoints {
		m[ep.protocol] = append(m[ep.protocol], ep.listener.Addr().String())
	}
	return m
}
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.closing)
		for _, ep := range s.endpoints {
			ep.listener.Close()
		}
		s.admission.closeAll()
	})
}
func (s *Server) Serve(ctx context.Context) error {
	errCh := make(chan error, len(s.endpoints))
	for _, ep := range s.endpoints {
		s.wg.Add(1)
		go func(ep endpoint) { defer s.wg.Done(); errCh <- s.accept(ep) }(ep)
	}
	var err error
	select {
	case <-ctx.Done():
	case <-s.closing:
	case err = <-errCh:
	case err = <-s.log.Errors():
	}
	s.Close()
	s.wg.Wait()
	return err
}
func (s *Server) accept(ep endpoint) error {
	for {
		c, err := ep.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		select {
		case <-s.closing:
			c.Close()
			return nil
		default:
		}
		if !s.admission.acquire(c) {
			c.Close()
			continue
		}
		s.wg.Add(1)
		go func() { defer s.wg.Done(); defer s.admission.release(c); defer c.Close(); s.handle(c, ep.protocol) }()
	}
}
func (s *Server) handle(raw net.Conn, protocol string) {
	// Close independently of reads: SSH channel windows and request queues can stall above the socket.
	lifetime := time.AfterFunc(s.cfg.MaxLifetime, func() { raw.Close() })
	defer lifetime.Stop()
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return
	}
	ip, _, _ := net.SplitHostPort(raw.RemoteAddr().String())
	port := raw.LocalAddr().(*net.TCPAddr).Port
	base := eventlog.Event{Session: hex.EncodeToString(idBytes), IP: ip, Port: port, Protocol: protocol}
	emit := func(kind, room, text string) { e := base; e.Kind = kind; e.Room = room; e.Text = text; s.log.Emit(e) }
	emit("connect", s.world.Start, "")
	defer func() { emit("disconnect", "", "") }()
	g := guard(raw, s.cfg)
	session := adventure.NewSession(s.world, emit)
	if protocol == "ssh" {
		s.handleSSH(g, session, base, emit)
		return
	}
	var c net.Conn = g
	if protocol == "tls" {
		t := tls.Server(g, s.tlsConfig)
		if err := t.Handshake(); err != nil {
			emit("handshake_error", "", err.Error())
			return
		}
		c = t
	}
	g.phase.Store(0)
	s.handlePlain(c, g, session, emit)
}
