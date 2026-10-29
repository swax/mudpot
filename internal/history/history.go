// Package history keeps permanent progress summaries and expires event detail.
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/swax/mudpot/internal/eventlog"
	_ "modernc.org/sqlite"
)

const (
	PageSize          = 50
	EventPageSize     = 100
	RoutineRetention  = 7 * 24 * time.Hour
	GameplayRetention = 180 * 24 * time.Hour
	cleanupBatch      = 128
)

func Path(logPath string) string { return logPath + ".sqlite" }

type Store struct {
	db          *sql.DB
	mu          sync.Mutex
	nextCleanup time.Time
	maintenance bool
}

type Progress struct {
	StartRoom                             string `json:"start_room,omitempty"`
	Adventure                             string `json:"adventure,omitempty"`
	TotalRooms                            int    `json:"total_rooms,omitempty"`
	Rooms, Clues, Items, Puzzles, Rewards []string
	Route                                 []string
	RouteTruncated                        bool `json:"route_truncated,omitempty"`
}

type Session struct {
	ID, IP, Protocol, Room, Status, User                     string
	Port, Commands, EventCount, Progress, Victories, Limited int
	Start, End                                               time.Time
	Detail                                                   Progress
	Events                                                   []eventlog.Event
	RetainedEvents                                           int
}

func (s *Session) Milestone() string {
	return []string{"No game progress", "Read clues", "Explored", "Collected items", "Solved a puzzle", "Recovered a reward"}[min(max(s.Progress, 0), 5)]
}
func (s *Session) Duration() string { return s.End.Sub(s.Start).Round(time.Second).String() }
func (s *Session) score() int {
	return s.Progress*1000000 + len(s.Detail.Rooms)*1000 + len(s.Detail.Puzzles)*10 + len(s.Detail.Items)
}

type Host struct {
	IP                            string
	Connections, Plays, Victories int
	First, Last                   time.Time
	Best                          *Session
}

type Count struct {
	Name  string
	Value int
}
type Daily struct {
	Day                                             string
	Sessions, Players, Commands, Victories, Limited int
}
type Query struct {
	View, Search, Activity, Session, Host, Before, After string
	EventPage                                            int
}
type View struct {
	Hosts                                                             []*Host
	Sessions                                                          []*Session
	Selected                                                          *Session
	Total, Unique, Commands, Victories, Limited, Players, PlayerHosts int
	Next, Previous                                                    string
	EventPage, EventPages                                             int
	Rooms, TopCommands                                                []Count
	Days                                                              []Daily
	SearchLimited                                                     bool
	Query                                                             Query
	Updated                                                           time.Time
}

func open(path string, readOnly bool) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	uriPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := url.Values{"mode": {mode}, "_pragma": {"busy_timeout(500)", "cache_size(-2048)", "temp_store(FILE)"}}
	if readOnly {
		q.Add("_pragma", "query_only(ON)")
	}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("session history must be a regular file")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return nil, err
	}
	err = f.Chmod(0640)
	f.Close()
	if err != nil {
		return nil, err
	}
	s, err := open(path, false)
	if err != nil {
		return nil, err
	}
	if err = s.migrate(context.Background()); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }

func addUnique(values *[]string, value string) {
	// World identifiers are limited to 64 bytes; bound imported data too.
	if value != "" && len(value) <= 256 && len(*values) < 256 && !slices.Contains(*values, value) {
		*values = append(*values, value)
	}
}
func (s *Session) apply(e eventlog.Event) {
	if s.Start.IsZero() || e.Time.Before(s.Start) {
		s.Start = e.Time
	}
	latest := !e.Time.Before(s.End)
	if latest {
		s.End = e.Time
		if e.Room != "" {
			s.Room = e.Room
		}
	}
	if s.Detail.StartRoom == "" && e.Room != "" {
		s.Detail.StartRoom = e.Room
	}
	if e.Adventure != "" {
		s.Detail.Adventure = e.Adventure
		s.Detail.TotalRooms = e.WorldRooms
	}
	if e.IP != "" {
		s.IP = e.IP
	}
	if e.Protocol != "" {
		s.Protocol = e.Protocol
	}
	if e.Port != 0 {
		s.Port = e.Port
	}
	if e.User != "" {
		s.User = e.User
	}
	s.EventCount++
	if latest {
		switch e.Kind {
		case "connect":
			s.Status = "connected"
		case "disconnect":
			if s.Status == "connected" || s.Status == "observed" {
				s.Status = "disconnected"
			}
		case "victory":
			s.Status = "vault recovered"
		case "limit":
			s.Status = "limited"
		case "handshake_error":
			s.Status = "handshake failed"
		}
	}
	rank := 0
	switch e.Kind {
	case "command":
		s.Commands++
	case "read":
		rank = 1
		addUnique(&s.Detail.Clues, e.Text)
	case "move":
		rank = 2
	case "take":
		rank = 3
		addUnique(&s.Detail.Items, e.Text)
	case "puzzle":
		rank = 4
		addUnique(&s.Detail.Puzzles, e.Text)
	case "victory", "reward":
		rank = 5
		addUnique(&s.Detail.Rewards, e.Text)
	}
	if e.Kind == "victory" {
		s.Victories++
	}
	if e.Kind == "limit" {
		s.Limited++
	}
	if rank > 0 {
		if s.Progress == 0 {
			addUnique(&s.Detail.Rooms, s.Detail.StartRoom)
			if s.Detail.StartRoom != "" {
				s.Detail.Route = append(s.Detail.Route, s.Detail.StartRoom)
			}
		}
		s.Progress = max(s.Progress, rank)
		addUnique(&s.Detail.Rooms, e.Room)
		if e.Kind == "move" {
			if len(s.Detail.Route) < 64 {
				s.Detail.Route = append(s.Detail.Route, e.Room)
			} else {
				s.Detail.RouteTruncated = true
			}
		}
	}
}

const sessionColumns = `s.id,s.ip,s.protocol,s.port,s.room,s.user,s.status,s.start,s.end,s.commands,s.event_count,s.progress,s.victories,s.limited,s.summary`

func scanSession(row interface{ Scan(...any) error }, prefix ...any) (*Session, error) {
	s := &Session{}
	var start, end int64
	var summary []byte
	if err := row.Scan(append(prefix, &s.ID, &s.IP, &s.Protocol, &s.Port, &s.Room, &s.User, &s.Status, &start, &end, &s.Commands, &s.EventCount, &s.Progress, &s.Victories, &s.Limited, &summary)...); err != nil {
		return nil, err
	}
	s.Start, s.End = time.Unix(0, start).UTC(), time.Unix(0, end).UTC()
	if err := json.Unmarshal(summary, &s.Detail); err != nil {
		return nil, err
	}
	return s, nil
}
