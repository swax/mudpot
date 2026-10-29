// Package eventlog writes bounded JSON records through a bounded asynchronous queue.
package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Event struct {
	Time       time.Time `json:"time"`
	Session    string    `json:"session,omitempty"`
	IP         string    `json:"ip,omitempty"`
	Port       int       `json:"port,omitempty"`
	Protocol   string    `json:"protocol,omitempty"`
	Kind       string    `json:"kind"`
	Room       string    `json:"room,omitempty"`
	Text       string    `json:"text,omitempty"`
	User       string    `json:"user,omitempty"`
	Adventure  string    `json:"adventure,omitempty"`
	WorldRooms int       `json:"world_rooms,omitempty"`
}
type Logger struct {
	recorder Recorder
	queue    chan Event
	done     chan struct{}
	errors   chan error
	mu       sync.RWMutex
	closed   bool
	failed   atomic.Bool
	dropped  atomic.Uint64
}

// Recorder persists batches before their rotating log records are written.
type Recorder interface{ Record([]Event) error }

func truncate(s string, max int) string {
	if len(s) > max {
		s = s[:max]
	}
	return strings.ToValidUTF8(s, "?")
}
func Open(path string, maxBytes int64) (*Logger, error) {
	return OpenRecorded(path, maxBytes, nil)
}

func OpenRecorded(path string, maxBytes int64, recorder Recorder) (*Logger, error) {
	if maxBytes < 4096 {
		return nil, fmt.Errorf("log size must be at least 4096 bytes")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("log must be a regular file")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0640); err != nil {
		f.Close()
		return nil, err
	}
	l := &Logger{recorder: recorder, queue: make(chan Event, 1024), done: make(chan struct{}), errors: make(chan error, 1)}
	go l.run(path, f, maxBytes)
	return l, nil
}
func (l *Logger) Emit(e Event) {
	e.Time = time.Now().UTC()
	e.Text = truncate(e.Text, 1024)
	e.User = truncate(e.User, 128)
	e.Adventure = truncate(e.Adventure, 128)
	if e.WorldRooms < 0 || e.WorldRooms > 256 {
		e.WorldRooms = 0
	}
	e.Room = truncate(e.Room, 64)
	e.Kind = truncate(e.Kind, 32)
	e.Session = truncate(e.Session, 64)
	e.IP = truncate(e.IP, 64)
	e.Protocol = truncate(e.Protocol, 16)
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.closed || l.failed.Load() {
		return
	}
	if l.recorder != nil {
		// Apply backpressure rather than silently losing a player's history when
		// storage falls behind. The bounded connection and queue limits still apply.
		select {
		case l.queue <- e:
		case <-l.done:
		}
		return
	}
	select {
	case l.queue <- e:
	default:
		l.dropped.Add(1)
	}
}
func (l *Logger) Errors() <-chan error { return l.errors }
func (l *Logger) Dropped() uint64      { return l.dropped.Load() }
func (l *Logger) Close() {
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		close(l.queue)
	}
	l.mu.Unlock()
	<-l.done
}
func (l *Logger) run(path string, file *os.File, maxBytes int64) {
	defer close(l.done)
	defer func() {
		if file != nil {
			file.Close()
		}
	}()
	info, err := file.Stat()
	var size int64
	if err == nil {
		size = info.Size()
	}
	fail := func(err error) { l.failed.Store(true); l.errors <- fmt.Errorf("event log failed: %w", err) }
	if err != nil {
		fail(err)
		return
	}
	for e := range l.queue {
		batch := make([]Event, 1, 128)
		batch[0] = e
		// One durable commit for a short burst, rather than an fsync per probe.
		var timer *time.Timer
		var deadline <-chan time.Time
		if l.recorder != nil {
			timer = time.NewTimer(100 * time.Millisecond)
			deadline = timer.C
		}
		for len(batch) < cap(batch) {
			select {
			case next, ok := <-l.queue:
				if !ok {
					goto ready
				}
				batch = append(batch, next)
			default:
				if deadline == nil {
					goto ready
				}
				select {
				case next, ok := <-l.queue:
					if !ok {
						goto ready
					}
					batch = append(batch, next)
				case <-deadline:
					goto ready
				}
			}
		}
	ready:
		if timer != nil {
			timer.Stop()
		}
		if l.recorder != nil {
			if err := l.recorder.Record(batch); err != nil {
				fail(fmt.Errorf("session history: %w", err))
				return
			}
		}
		for _, e := range batch {
			b, err := json.Marshal(e)
			if err != nil {
				fail(err)
				return
			}
			b = append(b, '\n')
			if int64(len(b)) > maxBytes {
				e.Text = truncate(e.Text, 128)
				b, err = json.Marshal(e)
				if err != nil {
					fail(err)
					return
				}
				b = append(b, '\n')
			}
			if size+int64(len(b)) > maxBytes {
				if err = file.Close(); err == nil {
					if removeErr := os.Remove(path + ".1"); removeErr != nil && !os.IsNotExist(removeErr) {
						err = removeErr
					}
				}
				if err == nil {
					err = os.Rename(path, path+".1")
				}
				if err == nil {
					file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
				}
				if err != nil {
					fail(err)
					return
				}
				size = 0
			}
			n, err := file.Write(b)
			if err != nil {
				fail(err)
				return
			}
			size += int64(n)
		}
	}
	if err := file.Sync(); err != nil {
		fail(err)
	}
}
