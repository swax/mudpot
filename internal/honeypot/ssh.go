package honeypot

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/swax/mudpot/internal/adventure"
	"github.com/swax/mudpot/internal/eventlog"
	"golang.org/x/crypto/ssh"
)

func (s *Server) handleSSH(g *guardedConn, session *adventure.Session, base eventlog.Event, emit adventure.Emit) {
	config := *s.sshConfig
	auth := func(meta ssh.ConnMetadata, method, credential string) (*ssh.Permissions, error) {
		if len(meta.User()) > 128 || len(credential) > 256 {
			return nil, fmt.Errorf("credential length exceeded")
		}
		e := base
		e.Kind = "auth"
		e.User = meta.User()
		e.Text = method
		if s.cfg.CapturePasswords && method == "password" {
			e.Text += " password=" + credential
		}
		s.log.Emit(e)
		return nil, nil
	}
	config.PasswordCallback = func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		return auth(meta, "password", string(password))
	}
	config.PublicKeyCallback = func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		return auth(meta, "publickey", ssh.FingerprintSHA256(key))
	}
	config.KeyboardInteractiveCallback = func(meta ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		answers, err := challenge(meta.User(), "Station access", []string{"Password: "}, []bool{false})
		if err != nil {
			return nil, err
		}
		if len(answers) != 1 {
			return nil, fmt.Errorf("invalid answers")
		}
		return auth(meta, "password", answers[0])
	}
	conn, channels, requests, err := ssh.NewServerConn(g, &config)
	if err != nil {
		emit("handshake_error", "", err.Error())
		return
	}
	defer conn.Close()
	g.phase.Store(0)
	g.Conn.SetReadDeadline(g.deadline(g.idle))
	// Reject every global request (including forwarding). A request flood closes the connection.
	go func() {
		count := 0
		for req := range requests {
			count++
			if count > 128 {
				conn.Close()
				return
			}
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}()
	accepted := false
	finished := make(chan struct{})
	defer func() {
		conn.Close()
		if accepted {
			<-finished
		}
	}()
	for {
		var next ssh.NewChannel
		select {
		case <-finished:
			return
		case incoming, ok := <-channels:
			if !ok {
				return
			}
			next = incoming
		}
		if next.ChannelType() != "session" || accepted {
			next.Reject(ssh.Prohibited, "Only one recovery session is available")
			continue
		}
		channel, channelRequests, err := next.Accept()
		if err != nil {
			return
		}
		accepted = true
		go func() {
			defer close(finished)
			defer channel.Close()
			s.serveSSHChannel(channel, channelRequests, session, emit)
		}()
	}
}
func (s *Server) serveSSHChannel(channel ssh.Channel, requests <-chan *ssh.Request, session *adventure.Session, emit adventure.Emit) {
	pty := false
	count := 0
	for req := range requests {
		count++
		if count > 32 {
			return
		}
		switch req.Type {
		case "pty-req":
			if len(req.Payload) > 4096 {
				return
			}
			pty = true
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "window-change":
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "shell":
			if len(req.Payload) != 0 {
				return
			}
			if req.WantReply {
				req.Reply(true, nil)
			}
			go func() {
				count := 0
				for req := range requests {
					count++
					if count > 128 {
						channel.Close()
						return
					}
					if req.WantReply {
						req.Reply(req.Type == "window-change", nil)
					}
				}
			}()
			s.serveTerminal(channel, bufio.NewReaderSize(channel, 4096), session, emit, pty)
			return
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil || len(payload.Command) > 256 {
				emit("limit", session.Room, "SSH exec length")
				return
			}
			if req.WantReply {
				req.Reply(true, nil)
			}
			result := session.Run(payload.Command)
			io.WriteString(channel, session.World.Intro+"\n\n"+strings.ReplaceAll(result.Text, "\n", "\r\n"))
			if !result.Quit {
				io.WriteString(channel, "\r\nOpen an interactive SSH shell to continue the journey and keep your inventory.\r\n")
			}
			channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
			return
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}
