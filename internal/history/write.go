package history

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/swax/mudpot/internal/eventlog"
)

type change struct {
	entry                     *Session
	oldIP                     string
	oldProgress, oldVictories int
	fresh                     bool
	termsDirty                bool
	dirty                     bool
}

func (s *Store) Record(events []eventlog.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.record(context.Background(), events); err != nil {
		return err
	}
	if !s.maintenance {
		return nil
	}
	// Amortize expiry work against new writes; never perform an unbounded sweep.
	_, err := s.cleanup(context.Background(), time.Now().UTC(), min(len(events), cleanupBatch))
	return err
}

func (s *Store) record(ctx context.Context, events []eventlog.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	insert, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO events(fingerprint,session_id,time,kind,text,payload,expires) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	updates := map[string]*change{}
	days := map[string]*Daily{}
	rooms := map[string]int{}
	commands := map[string]int{}
	for _, e := range events {
		if e.Session == "" {
			continue
		}
		payload, err := json.Marshal(e)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(payload)
		c := updates[e.Session]
		if c == nil {
			entry, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions s WHERE id=?`, e.Session))
			fresh := errors.Is(err, sql.ErrNoRows)
			if err != nil && !fresh {
				return err
			}
			if fresh {
				entry = &Session{ID: e.Session, Status: "observed"}
			}
			c = &change{entry: entry, oldIP: entry.IP, oldProgress: entry.Progress, oldVictories: entry.Victories, fresh: fresh}
			updates[e.Session] = c
		}
		rank := c.entry.Progress
		retention := RoutineRetention
		if rank > 0 || isProgress(e.Kind) {
			retention = GameplayRetention
		}
		result, err := insert.ExecContext(ctx, hash[:], e.Session, e.Time.UnixNano(), e.Kind, e.Text, payload, e.Time.Add(retention).UnixNano())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		c.dirty = true
		day := e.Time.UTC().Format("2006-01-02")
		d := days[day]
		if d == nil {
			d = &Daily{Day: day}
			days[day] = d
		}
		if c.fresh {
			d.Sessions++
			c.fresh = false
		}
		oldUser := c.entry.User
		oldAdventure := c.entry.Detail.Adventure
		oldTerms := len(c.entry.Detail.Rooms) + len(c.entry.Detail.Clues) + len(c.entry.Detail.Items) + len(c.entry.Detail.Puzzles) + len(c.entry.Detail.Rewards)
		c.entry.apply(e)
		if rank == 0 && c.entry.Progress > 0 {
			d.Players++
			// Promotion preserves the beginning of a session, before its first
			// successful game action. Do this once, not on every command.
			if _, err = tx.ExecContext(ctx, `UPDATE events SET expires=time+? WHERE session_id=?`, int64(GameplayRetention), e.Session); err != nil {
				return err
			}
		}
		newTerms := len(c.entry.Detail.Rooms) + len(c.entry.Detail.Clues) + len(c.entry.Detail.Items) + len(c.entry.Detail.Puzzles) + len(c.entry.Detail.Rewards)
		c.termsDirty = c.termsDirty || oldTerms != newTerms || oldUser != c.entry.User || oldAdventure != c.entry.Detail.Adventure || rank != c.entry.Progress
		if e.Kind == "command" {
			d.Commands++
			commands[commandCategory(e.Text)]++
		}
		if e.Kind == "victory" {
			d.Victories++
		}
		if e.Kind == "limit" {
			d.Limited++
		}
		if e.Kind == "move" {
			rooms[e.Room]++
		}
	}
	for _, c := range updates {
		entry := c.entry
		if !c.dirty {
			continue
		}
		b, err := json.Marshal(entry.Detail)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,ip,protocol,port,room,user,status,start,end,commands,victories,limited,event_count,progress,summary)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET ip=excluded.ip,protocol=excluded.protocol,port=excluded.port,
 room=excluded.room,user=excluded.user,status=excluded.status,start=excluded.start,end=excluded.end,commands=excluded.commands,
 victories=excluded.victories,limited=excluded.limited,event_count=excluded.event_count,progress=excluded.progress,summary=excluded.summary`,
			entry.ID, entry.IP, entry.Protocol, entry.Port, entry.Room, entry.User, entry.Status, entry.Start.UnixNano(), entry.End.UnixNano(), entry.Commands, entry.Victories, entry.Limited, entry.EventCount, entry.Progress, b)
		if err != nil {
			return err
		}
		// A new session's first event was counted in its daily bucket. The old
		// IP is empty for new sessions, including late-arriving transport metadata.
		connections := 0
		if c.oldIP == "" && entry.IP != "" {
			connections = 1
		}
		plays := 0
		if c.oldProgress == 0 && entry.Progress > 0 {
			plays = 1
		}
		if err = updateHost(ctx, tx, entry, connections, plays, entry.Victories-c.oldVictories); err != nil {
			return err
		}
		if c.termsDirty && entry.Progress > 0 {
			if err = writeTerms(ctx, tx, entry); err != nil {
				return err
			}
		}
	}
	for _, d := range days {
		_, err = tx.ExecContext(ctx, `INSERT INTO daily VALUES(?,?,?,?,?,?) ON CONFLICT(day) DO UPDATE SET sessions=sessions+excluded.sessions,
 players=players+excluded.players,commands=commands+excluded.commands,victories=victories+excluded.victories,limited=limited+excluded.limited`, d.Day, d.Sessions, d.Players, d.Commands, d.Victories, d.Limited)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE totals SET sessions=sessions+?,players=players+?,commands=commands+?,victories=victories+?,limited=limited+? WHERE id=1`, d.Sessions, d.Players, d.Commands, d.Victories, d.Limited)
		if err != nil {
			return err
		}
	}
	for name, total := range rooms {
		if _, err = tx.ExecContext(ctx, `INSERT INTO room_counts VALUES(?,?) ON CONFLICT(name) DO UPDATE SET total=total+excluded.total`, name, total); err != nil {
			return err
		}
	}
	for name, total := range commands {
		if _, err = tx.ExecContext(ctx, `INSERT INTO command_counts VALUES(?,?) ON CONFLICT(name) DO UPDATE SET total=total+excluded.total`, name, total); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func isProgress(kind string) bool {
	switch kind {
	case "read", "move", "take", "puzzle", "victory", "reward":
		return true
	}
	return false
}

func updateHost(ctx context.Context, tx *sql.Tx, s *Session, connections, plays, victories int) error {
	if s.IP == "" {
		return nil
	}
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO hosts VALUES(?,0,0,0,0,0,0,'')`, s.IP)
	if err != nil {
		return err
	}
	added, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if added > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE totals SET hosts=hosts+1 WHERE id=1`); err != nil {
			return err
		}
	}
	first, last := int64(0), int64(0)
	if s.Progress > 0 {
		first = s.Start.UnixNano()
		last = s.End.UnixNano()
	}
	if plays > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE totals SET player_hosts=player_hosts+1 WHERE id=1 AND EXISTS(SELECT 1 FROM hosts WHERE ip=? AND plays=0)`, s.IP); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE hosts SET connections=connections+?,plays=plays+?,victories=victories+?,
 first_play=CASE WHEN ?<>0 AND (first_play=0 OR ?<first_play) THEN ? ELSE first_play END,
 last_play=max(last_play,?),best_session=CASE WHEN ?>0 AND ?>=best_score THEN ? ELSE best_session END,
 best_score=max(best_score,?) WHERE ip=?`, connections, plays, victories, first, first, first, last, s.Progress, s.score(), s.ID, s.score(), s.IP)
	return err
}

func writeTerms(ctx context.Context, tx *sql.Tx, s *Session) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM search_terms WHERE session_id=?`, s.ID); err != nil {
		return err
	}
	values := []string{s.ID, s.IP, s.User, s.Room, s.Detail.Adventure}
	values = append(values, s.Detail.Rooms...)
	values = append(values, s.Detail.Clues...)
	values = append(values, s.Detail.Items...)
	values = append(values, s.Detail.Puzzles...)
	values = append(values, s.Detail.Rewards...)
	seen := map[string]bool{}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO search_terms VALUES(?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, value := range values {
		value = strings.ToLower(value)
		terms := append([]string{value}, strings.Fields(value)...)
		terms = append(terms, strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })...)
		for _, term := range terms {
			if term == "" || seen[term] {
				continue
			}
			seen[term] = true
			if _, err = stmt.ExecContext(ctx, term, s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// Only a fixed vocabulary reaches permanent command counters. Arbitrary scanner
// arguments and random command names cannot grow a table indefinitely.
func commandCategory(text string) string {
	fields := strings.Fields(strings.ToLower(text))
	if len(fields) == 0 {
		return "other"
	}
	switch fields[0] {
	case "north", "south", "east", "west", "up", "down", "n", "s", "e", "w", "u", "d", "go", "move", "walk":
		return "movement"
	case "read", "examine", "cat", "open", "check", "inspect":
		return "read / inspect"
	case "take", "grab", "get", "pickup":
		return "collect"
	case "use":
		return "use"
	case "look", "l":
		return "look"
	case "inventory", "inv", "i":
		return "inventory"
	case "help", "?":
		return "help"
	case "quit", "exit", "logout", "q":
		return "quit"
	case "whoami", "id", "pwd", "ls", "hostname", "uname":
		return "system probe"
	default:
		return "other"
	}
}

// Maintain performs one small sweep per tick, yielding to active writes. It does
// not vacuum/rewrite the database; SQLite reuses the released pages.
func (s *Store) Maintain(ctx context.Context) error {
	s.mu.Lock()
	s.maintenance = true
	s.mu.Unlock()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if !s.mu.TryLock() {
				continue
			}
			_, err := s.cleanup(ctx, now.UTC(), cleanupBatch)
			s.mu.Unlock()
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// Cleanup is also exposed for deterministic retention tests and maintenance.
func (s *Store) Cleanup(ctx context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextCleanup = time.Time{}
	return s.cleanup(ctx, now, cleanupBatch)
}
func (s *Store) cleanup(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 || now.Before(s.nextCleanup) {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id IN(SELECT id FROM events WHERE expires<? ORDER BY expires LIMIT ?)`, now.UnixNano(), limit)
	if err != nil {
		return 0, err
	}
	events, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	result, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE id IN(SELECT id FROM sessions WHERE progress=0 AND end<? ORDER BY end LIMIT ?)`, now.Add(-RoutineRetention).UnixNano(), limit)
	if err != nil {
		return 0, err
	}
	sessions, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	s.nextCleanup = now
	if events < int64(limit) && sessions < int64(limit) {
		s.nextCleanup = now.Add(time.Minute)
	}
	return int(events + sessions), nil
}
