package honeypot

import (
	"bufio"
	"errors"
	"io"
	"unicode/utf8"
)

var errLine = errors.New("line exceeds 256 bytes")
var errQuit = errors.New("terminal disconnect")

// terminal retains Telnet negotiation and CRLF state across fragmented socket reads.
type terminal struct {
	r           *bufio.Reader
	iac, escape int
	lastCR      bool
	echo        io.Writer
}

func (t *terminal) line() (string, error) {
	line := make([]byte, 0, 256)
	for {
		b, err := t.r.ReadByte()
		if err != nil {
			return "", err
		}
		switch t.iac {
		case 1:
			switch b {
			case 244:
				return "", errQuit
			case 250:
				t.iac = 3
			case 251, 252, 253, 254:
				t.iac = 2
			default:
				t.iac = 0
			}
			continue
		case 2:
			t.iac = 0
			continue
		case 3:
			if b == 255 {
				t.iac = 4
			}
			continue
		case 4:
			if b == 240 {
				t.iac = 0
			} else {
				t.iac = 3
			}
			continue
		}
		if b == 255 {
			t.iac = 1
			continue
		}
		if b == 3 || b == 4 {
			return "", errQuit
		}
		if t.escape == 1 {
			if b == '[' {
				t.escape = 2
			} else {
				t.escape = 0
			}
			continue
		}
		if t.escape == 2 {
			if b >= 0x40 && b <= 0x7e {
				t.escape = 0
			}
			continue
		}
		if b == 27 {
			t.escape = 1
			continue
		}
		if t.lastCR {
			t.lastCR = false
			if b == '\n' || b == 0 {
				continue
			}
		}
		if b == '\r' || b == '\n' {
			t.lastCR = b == '\r'
			if t.echo != nil {
				if _, err := io.WriteString(t.echo, "\r\n"); err != nil {
					return "", err
				}
			}
			return string(line), nil
		}
		if b == 127 || b == 8 {
			if len(line) > 0 {
				_, size := utf8.DecodeLastRune(line)
				line = line[:len(line)-size]
				if t.echo != nil {
					if _, err := io.WriteString(t.echo, "\b \b"); err != nil {
						return "", err
					}
				}
			}
			continue
		}
		if b < 32 {
			continue
		}
		if len(line) >= 256 {
			return "", errLine
		}
		line = append(line, b)
		if t.echo != nil && b < 127 {
			if _, err := t.echo.Write([]byte{b}); err != nil {
				return "", err
			}
		}
	}
}
