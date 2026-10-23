package honeypot

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swax/mudpot/internal/adventure"
	"github.com/swax/mudpot/internal/eventlog"
	"golang.org/x/crypto/ssh"
)

func startTestServer(t *testing.T, protocol string, mutate func(*Config)) (*Server, string, string) {
	t.Helper()
	cfg := Defaults()
	cfg.TCP = nil
	cfg.SSH = nil
	cfg.TLS = nil
	switch protocol {
	case "tcp":
		cfg.TCP = []string{"127.0.0.1:0"}
	case "ssh":
		cfg.SSH = []string{"127.0.0.1:0"}
	case "tls":
		cfg.TLS = []string{"127.0.0.1:0"}
	}
	dir := t.TempDir()
	cfg.StateDir = filepath.Join(dir, "state")
	if mutate != nil {
		mutate(&cfg)
	}
	w, err := adventure.Load("")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "events.jsonl")
	logger, err := eventlog.Open(path, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, w, logger)
	if err != nil {
		logger.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		s.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server shutdown stalled")
		}
		logger.Close()
	})
	return s, s.Addresses()[protocol][0], path
}
func dial(t *testing.T, address string) net.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	t.Cleanup(func() { c.Close() })
	return c
}
func until(t *testing.T, r *bufio.Reader, end string) string {
	t.Helper()
	var b strings.Builder
	for b.Len() < 65536 {
		c, err := r.ReadByte()
		if err != nil {
			t.Fatalf("waiting for %q: %v, got %q", end, err, b.String())
		}
		b.WriteByte(c)
		if strings.HasSuffix(b.String(), end) {
			return b.String()
		}
	}
	t.Fatal("response too large")
	return ""
}
func TestTCPAdventureEndToEnd(t *testing.T) {
	_, address, _ := startTestServer(t, "tcp", nil)
	c := dial(t, address)
	r := bufio.NewReader(c)
	if out := until(t, r, "> "); !strings.Contains(out, "SSH PRIVATE KEY") {
		t.Fatal("no reward promise")
	}
	walk := []string{"east", "take access_crystal", "west", "north", "west", "take security_seal", "north", "east", "use security_seal", "the one", "north", "use root_console"}
	for i, cmd := range walk {
		io.WriteString(c, cmd+"\r\n")
		prompt := "> "
		if i == 8 {
			prompt = "We live for... we die for... > "
		}
		if i == len(walk)-1 {
			prompt = "root@g-sector-lwr:~# "
		}
		out := until(t, r, prompt)
		if i == len(walk)-1 && !strings.Contains(out, "Administrator access granted") {
			t.Fatal(out)
		}
	}
	io.WriteString(c, "cat /root/.ssh/id_ed25519\n")
	out := until(t, r, "root@g-sector-lwr:~# ")
	if !strings.Contains(out, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatal(out)
	}
	io.WriteString(c, "quit\n")
	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}
}
func TestFragmentedHTTPAndNoDashboardExposure(t *testing.T) {
	_, address, _ := startTestServer(t, "tcp", nil)
	for _, path := range []string{"/", "/.env", "/dashboard"} {
		c := dial(t, address)
		for _, chunk := range []string{"G", "ET " + path + " HTTP/1.1\r\nHost: station.invalid\r\n", "\r\n"} {
			if _, err := io.WriteString(c, chunk); err != nil {
				t.Fatal(err)
			}
		}
		b, err := io.ReadAll(c)
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(b, []byte("HTTP/1.1 200")) || !bytes.Contains(b, []byte("SSH PRIVATE KEY")) || bytes.Contains(b, []byte("Private Monitor")) {
			t.Fatal(string(b))
		}
	}
}
func TestFTPAndMUDShareCommandLimits(t *testing.T) {
	for _, ftp := range []bool{false, true} {
		t.Run(map[bool]string{false: "mud", true: "ftp"}[ftp], func(t *testing.T) {
			_, address, _ := startTestServer(t, "tcp", func(c *Config) { c.CommandsPerMinute = 2 })
			c := dial(t, address)
			if ftp {
				io.WriteString(c, "USER bot\r\nPASS secret\r\nLIST\r\n")
			} else {
				io.WriteString(c, "look\nlook\nlook\n")
			}
			b, err := io.ReadAll(c)
			if err != nil {
				t.Fatal(err)
			}
			if !ftp && !bytes.Contains(b, []byte("command limit")) {
				t.Fatal(string(b))
			}
			if ftp && bytes.Contains(b, []byte("Recovery catalog")) {
				t.Fatal("FTP rate limit bypass")
			}
		})
	}
}
func TestOversizedAndSlowInputsClose(t *testing.T) {
	_, address, _ := startTestServer(t, "tcp", func(c *Config) {
		c.HandshakeTimeout = 150 * time.Millisecond
		c.IdleTimeout = 200 * time.Millisecond
		c.MaxLifetime = time.Second
	})
	c := dial(t, address)
	io.WriteString(c, strings.Repeat("x", 257)+"\n")
	b, err := io.ReadAll(c)
	if err != nil || !bytes.Contains(b, []byte("line too long")) {
		t.Fatal(string(b), err)
	}
	c = dial(t, address)
	io.WriteString(c, "GET /")
	started := time.Now()
	io.ReadAll(c)
	if time.Since(started) > time.Second {
		t.Fatal("slow HTTP probe kept connection")
	}
}
func TestConnectionLimitIncludesSSHAndTLSHandshakes(t *testing.T) {
	for _, protocol := range []string{"ssh", "tls"} {
		t.Run(protocol, func(t *testing.T) {
			s, address, _ := startTestServer(t, protocol, func(c *Config) { c.MaxConnections = 1; c.PerIP = 1; c.HandshakeTimeout = 300 * time.Millisecond })
			first := dial(t, address)
			defer first.Close()
			deadline := time.Now().Add(time.Second)
			for {
				s.admission.mu.Lock()
				active := len(s.admission.active)
				s.admission.mu.Unlock()
				if active == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("connection not admitted")
				}
				time.Sleep(time.Millisecond)
			}
			second := dial(t, address)
			b := make([]byte, 128)
			n, err := second.Read(b)
			if n != 0 || err == nil {
				t.Fatal("pre-auth connection bypass", n, err)
			}
			// The first handshake also has to expire, even though it sends no credentials.
			io.ReadAll(first)
		})
	}
}
func TestTLSHandshakeAndAdventure(t *testing.T) {
	_, address, _ := startTestServer(t, "tls", nil)
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) // self-signed honeypot certificate
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(c)
	if out := until(t, r, "> "); !strings.Contains(out, "ROOT SYSTEM ACCESS") {
		t.Fatal(out)
	}
	io.WriteString(c, "look\n")
	until(t, r, "> ")
}
func TestSSHSessionIsolationForwardingAndExec(t *testing.T) {
	_, address, _ := startTestServer(t, "ssh", nil)
	client, err := ssh.Dial("tcp", address, &ssh.ClientConfig{User: "root", Auth: []ssh.AuthMethod{ssh.Password("never-log-this")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if connection, err := client.Dial("tcp", "127.0.0.1:1"); err == nil {
		connection.Close()
		t.Fatal("forwarding allowed")
	}
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()
	if err = session.Shell(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(stdout)
	if out := until(t, r, "> "); !strings.Contains(out, "SSH PRIVATE KEY") {
		t.Fatal(out)
	}
	if extra, err := client.NewSession(); err == nil {
		extra.Close()
		t.Fatal("multiple SSH channels accepted")
	}
	io.WriteString(stdin, "look\n")
	until(t, r, "> ")
	io.WriteString(stdin, "quit\n")
	io.ReadAll(r)
	client.Close()
	client, err = ssh.Dial("tcp", address, &ssh.ClientConfig{User: "bot", Auth: []ssh.AuthMethod{ssh.Password("never-log-this")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err = client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	out, err := session.Output("cat /etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("root:x:")) || !bytes.Contains(out, []byte("don't see")) {
		t.Fatal(string(out))
	}
	if !bytes.Contains(out, []byte("Next command: go east")) || !bytes.Contains(out, []byte("interactive SSH shell")) {
		t.Fatal("SSH probe did not invite play", string(out))
	}
}
func TestPasswordsOptInAndAbsoluteLifetime(t *testing.T) {
	s, address, path := startTestServer(t, "ssh", func(c *Config) { c.IdleTimeout = time.Second; c.MaxLifetime = time.Second })
	client, err := ssh.Dial("tcp", address, &ssh.ClientConfig{User: "root\nforged", Auth: []ssh.AuthMethod{ssh.Password("never-log-this")}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	// Do not request a shell: lifetime must also bound channels stuck waiting for requests.
	done := make(chan error, 1)
	go func() { done <- client.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("absolute lifetime did not close SSH")
	}
	s.log.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("never-log-this")) {
		t.Fatal("password captured without opt-in")
	}
	if bytes.Contains(b, []byte("root\nforged")) {
		t.Fatal("raw newline log injection")
	}
}

func TestFTPPasswordCaptureIsOptIn(t *testing.T) {
	for _, capture := range []bool{false, true} {
		t.Run(map[bool]string{false: "redacted", true: "captured"}[capture], func(t *testing.T) {
			s, address, path := startTestServer(t, "tcp", func(c *Config) { c.CapturePasswords = capture })
			c := dial(t, address)
			io.WriteString(c, "USER bot\r\nPASS ftp-secret-sample\r\nQUIT\r\n")
			io.ReadAll(c)
			c.Close()
			s.Close()
			s.log.Close()
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(b, []byte("ftp-secret-sample")) != capture {
				t.Fatal("FTP capture setting ignored")
			}
		})
	}
}

func TestGuardedWireBudgets(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	g := guard(left, Defaults())
	g.maxRead = 4
	g.maxWrite = 4
	go func() { right.Write([]byte("abcde")) }()
	buf := make([]byte, 10)
	n, err := g.Read(buf)
	if err != nil || n != 4 {
		t.Fatal(n, err)
	}
	if _, err = g.Read(buf); err != errBudget {
		t.Fatal("input byte budget", err)
	}
	go func() { io.ReadFull(right, make([]byte, 4)) }()
	if _, err = g.Write([]byte("test")); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Write([]byte("x")); err != errBudget {
		t.Fatal("output byte budget", err)
	}
}
func TestTerminalFragmentationAndCRLF(t *testing.T) {
	data := []byte{255, 251, 3, 'l', 'o', 'o', 'k', '\r', '\n', 255, 250, 1, 2, 255, 240, 'h', 'e', 'l', 'p', '\n'}
	r := &oneByteReader{data: data}
	term := terminal{r: bufio.NewReader(r)}
	for _, want := range []string{"look", "help"} {
		got, err := term.line()
		if err != nil || got != want {
			t.Fatal(got, err)
		}
	}
}

type oneByteReader struct{ data []byte }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}
func FuzzTerminal(f *testing.F) {
	for _, data := range [][]byte{[]byte("look\r\n"), {255, 250, 255, 240}, []byte("\x1b[31mhello\n"), bytes.Repeat([]byte{'x'}, 257)} {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		term := terminal{r: bufio.NewReader(&oneByteReader{data: data})}
		line, _ := term.line()
		if len(line) > 256 {
			t.Fatal("line limit")
		}
	})
}
