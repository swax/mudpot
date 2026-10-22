package honeypot

import (
	"bufio"
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/swax/mudpot/internal/adventure"
)

func (s *Server) handlePlain(c net.Conn, g *guardedConn, session *adventure.Session, emit adventure.Emit) {
	r := bufio.NewReaderSize(c, 4096)
	// Wait for client-first probes, preserving fragmented requests. Silent terminals get a greeting.
	g.phase.Store(time.Now().Add(500 * time.Millisecond).UnixNano())
	b, err := r.Peek(1)
	g.phase.Store(0)
	if err != nil {
		if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
			return
		}
		r = bufio.NewReaderSize(c, 4096)
	} else {
		if b[0] == 0x16 {
			emit("probe", "", "TLS on plaintext port")
			return
		}
		if !strings.ContainsRune("GPHODUS", rune(b[0])) {
			s.serveTerminal(c, r, session, emit, false)
			return
		}
		g.phase.Store(time.Now().Add(s.cfg.HandshakeTimeout).UnixNano())
		prefix, prefixErr := r.Peek(4)
		if prefixErr != nil {
			return
		}
		first := string(prefix)
		if first == "GET " || first == "POST" || first == "HEAD" || first == "OPTI" || first == "PUT " || first == "DELE" || first == "PATC" {
			s.serveHTTP(c, r, session, emit)
			return
		}
		g.phase.Store(0)
		if first == "SSH-" {
			emit("probe", "", "SSH on plaintext port")
			io.WriteString(c, "SSH-2.0-GreySector\r\n")
			return
		}
		if strings.EqualFold(first, "USER") {
			s.serveFTP(c, r, session, emit)
			return
		}
	}
	s.serveTerminal(c, r, session, emit, false)
}
func (s *Server) serveTerminal(stream io.ReadWriter, r *bufio.Reader, session *adventure.Session, emit adventure.Emit, echo bool) {
	write := func(text string) bool {
		_, err := io.WriteString(stream, strings.ReplaceAll(text, "\n", "\r\n"))
		return err == nil
	}
	if !write(session.Greeting() + "\n" + session.Prompt()) {
		return
	}
	t := &terminal{r: r}
	if echo {
		t.echo = stream
	}
	budget := commandBudget{max: s.cfg.CommandsPerMinute}
	for {
		line, err := t.line()
		if err != nil {
			if err == errLine {
				emit("limit", session.Room, err.Error())
				write("Connection closed: line too long.\n")
			}
			return
		}
		if !budget.allow() {
			emit("limit", session.Room, "command rate or session budget")
			write("Connection closed: command limit.\n")
			return
		}
		result := session.Run(line)
		if !write(result.Text) || result.Quit {
			return
		}
		if !write("\n" + session.Prompt()) {
			return
		}
	}
}

var publicPage = template.Must(template.New("probe").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>{{.Title}} // Recovery</title><style>body{background:#090f16;color:#c8d8df;font:16px/1.7 monospace;max-width:850px;margin:7vh auto;padding:24px}h1{color:#73e8c6}pre{white-space:pre-wrap}a{color:#73e8c6}</style><h1>{{.Title}} // Administrator Recovery</h1><pre>{{.Intro}}</pre><h2>{{.Room.Name}}</h2><p>{{.Room.Description}}</p><p>Connect to the station's Telnet or SSH recovery terminal to begin.</p><p>Available routes: {{range $dir,$exit := .Room.Exits}}<a href="/recovery/{{$dir}}">{{$dir}}</a> {{end}}</p></html>`))

func (s *Server) serveHTTP(c net.Conn, r *bufio.Reader, session *adventure.Session, emit adventure.Emit) {
	// ReadRequest has no server header limit, so cap the entire parse before calling it.
	limited := &io.LimitedReader{R: r, N: 8192}
	req, err := http.ReadRequest(bufio.NewReaderSize(limited, 4096))
	if err != nil || limited.N == 0 {
		emit("limit", session.Room, "invalid or oversized HTTP headers")
		return
	}
	emit("probe", session.Room, req.Method+" "+req.URL.RequestURI())
	// No request bodies, filesystem routing, public API, or dashboard routes are served here.
	var body bytes.Buffer
	if err = publicPage.Execute(&body, struct {
		Title, Intro string
		Room         adventure.Room
	}{s.world.Title, s.world.Intro, s.world.Rooms[s.world.Start]}); err != nil {
		return
	}
	status := "200 OK"
	if req.Method != "GET" && req.Method != "HEAD" {
		status = "405 Method Not Allowed"
		body.Reset()
		body.WriteString("Use the Telnet or SSH recovery terminal.\n")
	}
	header := fmt.Sprintf("HTTP/1.1 %s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\nContent-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'\r\n\r\n", status, body.Len())
	if _, err = io.WriteString(c, header); err != nil {
		return
	}
	if req.Method != "HEAD" {
		c.Write(body.Bytes())
	}
}
func (s *Server) serveFTP(c net.Conn, r *bufio.Reader, session *adventure.Session, emit adventure.Emit) {
	t := &terminal{r: r}
	budget := commandBudget{max: s.cfg.CommandsPerMinute}
	for {
		line, err := t.line()
		if err != nil {
			return
		}
		if !budget.allow() {
			emit("limit", session.Room, "FTP command rate or session budget")
			return
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		logged := line
		if strings.EqualFold(fields[0], "PASS") && !s.cfg.CapturePasswords {
			logged = "PASS [redacted]"
		}
		emit("probe", session.Room, "FTP "+logged)
		response := "500 Unknown command. Try LIST or HELP.\r\n"
		switch strings.ToUpper(fields[0]) {
		case "USER":
			response = "331 Station recovery requires identification.\r\n"
		case "PASS":
			response = "230 Recovery terminal available. Complete the journey for root access and private keys.\r\n"
		case "SYST":
			response = "215 UNIX Type: L8\r\n"
		case "PWD":
			response = "257 \"/station/recovery\"\r\n"
		case "LIST", "NLST":
			response = "150 Recovery catalog\r\nroot_console.locked\r\nid_ed25519.locked\r\ncredentials.env.locked\r\nbackup_manifest.locked\r\n226 Connect using Telnet or SSH and complete recovery to unlock.\r\n"
		case "HELP", "RETR":
			response = "214 " + strings.ReplaceAll(s.world.Intro, "\n", "\r\n214 ") + "\r\n"
		case "QUIT":
			io.WriteString(c, "221 Goodbye.\r\n")
			return
		}
		if _, err = io.WriteString(c, response); err != nil {
			return
		}
	}
}
