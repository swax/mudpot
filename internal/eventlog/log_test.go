package eventlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

type blockedRecorder struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockedRecorder) Record([]Event) error {
	close(r.started)
	<-r.release
	return errors.New("storage unavailable")
}

func TestHistoryFailureUnblocksBackpressureAndStopsLogWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	r := &blockedRecorder{started: make(chan struct{}), release: make(chan struct{})}
	l, err := OpenRecorded(path, 4096, r)
	if err != nil {
		t.Fatal(err)
	}
	l.Emit(Event{Session: "first", Kind: "connect"})
	<-r.started
	// Fill the bounded queue while the first persistent batch is blocked.
	for i := 0; i < 1024; i++ {
		l.Emit(Event{Session: "waiting", Kind: "command"})
	}
	done := make(chan struct{})
	go func() {
		l.Emit(Event{Session: "backpressure", Kind: "connect"})
		close(done)
	}()
	close(r.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("history failure left Emit blocked")
	}
	l.Close()
	select {
	case err := <-l.Errors():
		if !strings.Contains(err.Error(), "session history: storage unavailable") {
			t.Fatal(err)
		}
	default:
		t.Fatal("history failure not reported")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatal("log written before history commit", info, err)
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
