package eventlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredLogCannotForgeRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := Open(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	text := "password\n{\"kind\":\"victory\"}\r\x1b[31m<script>alert(1)</script>"
	l.Emit(Event{Kind: "auth", Text: text})
	l.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "\n") != 1 {
		t.Fatal("log forging")
	}
	var e Event
	if err := json.Unmarshal(b, &e); err != nil || e.Kind != "auth" || e.Text != text {
		t.Fatal(e, err)
	}
}
func TestLogRotationAndBoundedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := Open(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		l.Emit(Event{Kind: "command", Text: strings.Repeat("x", 10000)})
	}
	l.Close()
	select {
	case err := <-l.Errors():
		t.Fatal(err)
	default:
	}
	for _, name := range []string{path, path + ".1"} {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		info, _ := f.Stat()
		if info.Size() > 4096 {
			t.Fatal("log exceeded budget")
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var e Event
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				t.Fatal(err)
			}
			if len(e.Text) > 1024 {
				t.Fatal("unbounded field")
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
