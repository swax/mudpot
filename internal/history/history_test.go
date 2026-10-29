package history

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swax/mudpot/internal/eventlog"
)

func openTest(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session history #1.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func record(t *testing.T, s *Store, events []eventlog.Event) {
	t.Helper()
	if err := s.Record(events); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string, q Query) *View {
	t.Helper()
	v, err := Read(context.Background(), path, q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func event(id, ip, kind, room, text string, when time.Time) eventlog.Event {
	return eventlog.Event{Session: id, IP: ip, Kind: kind, Room: room, Text: text, Time: when}
}

func TestGameplayClassificationProgressAndHostGrouping(t *testing.T) {
	s, path := openTest(t)
	now := time.Now().UTC()
	var events []eventlog.Event
	for i, cmd := range []string{"whoami", "ls", "look", "inventory", "random command"} {
		events = append(events, event("scanner", "192.0.2.9", "command", "dock", cmd, now.Add(time.Duration(i)*time.Second)))
	}
	for i, step := range [][3]string{{"connect", "dock", ""}, {"move", "cargo", "east"}, {"read", "cargo", "station_notes"}, {"take", "cargo", "access_crystal"}, {"puzzle", "bridge", "security_gate"}, {"move", "dock", "west"}} {
		e := event("player", "192.0.2.1", step[0], step[1], step[2], now.Add(time.Duration(i)*time.Second))
		e.User = "Commander O'Neil"
		if i == 0 {
			e.Adventure = "Grey Sector"
			e.WorldRooms = 7
		}
		events = append(events, e)
	}
	events = append(events, event("returning", "192.0.2.1", "read", "dock", "manual", now.Add(time.Minute)))
	record(t, s, events)
	record(t, s, events) // duplicate replay must not change counters
	v := read(t, path, Query{})
	if v.Total != 3 || v.Unique != 2 || v.Players != 2 || v.PlayerHosts != 1 || v.Commands != 5 || len(v.Hosts) != 1 || v.Hosts[0].Plays != 2 {
		t.Fatalf("classification/totals: %+v", v)
	}
	best := v.Hosts[0].Best
	if best.ID != "player" || best.Milestone() != "Solved a puzzle" || best.Room != "dock" || len(best.Detail.Rooms) != 3 || len(best.Detail.Items) != 1 || len(best.Detail.Puzzles) != 1 || best.Detail.TotalRooms != 7 {
		t.Fatalf("progress regressed on backtracking: %+v", best)
	}
	for _, prefix := range []string{"COMMANDER", "o'neil", "access_", "security_", "station_", "player"} {
		v := read(t, path, Query{Search: prefix})
		if len(v.Hosts) != 1 {
			t.Fatalf("prefix %q: %+v", prefix, v)
		}
	}
	for _, prefix := range []string{"' OR 1=1 --", "%", "random command", "whoami"} {
		if v := read(t, path, Query{Search: prefix}); len(v.Hosts) != 0 {
			t.Fatal("unexpected search", prefix)
		}
	}
	if v := read(t, path, Query{Host: "192.0.2.1", View: "sessions"}); len(v.Sessions) != 2 {
		t.Fatal("host sessions missing")
	}
	if v := read(t, path, Query{Activity: "victory"}); len(v.Hosts) != 0 {
		t.Fatal("puzzle classified as reward")
	}
}

func TestRetentionPreservesSummariesStatsAndPromotion(t *testing.T) {
	s, path := openTest(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	logPath := filepath.Join(t.TempDir(), "events.jsonl")
	if _, err := s.Import(context.Background(), logPath); err != nil {
		t.Fatal(err)
	}
	var events []eventlog.Event
	for _, spec := range []struct {
		id     string
		age    int
		played bool
	}{{"old-noise", 8, false}, {"recent-noise", 6, false}, {"player", 100, true}, {"old-player", 181, true}} {
		when := now.Add(-time.Duration(spec.age) * 24 * time.Hour)
		events = append(events, event(spec.id, "192.0.2.1", "connect", "dock", "", when), event(spec.id, "192.0.2.1", "command", "dock", "whoami", when.Add(time.Second)))
		if spec.played {
			events = append(events, event(spec.id, "192.0.2.1", "move", "cargo", "east", when.Add(2*time.Second)), event(spec.id, "192.0.2.1", "victory", "vault", "root_console", when.Add(3*time.Second)))
		}
	}
	record(t, s, events)
	before := read(t, path, Query{View: "stats"})
	for i := 0; i < 20; i++ {
		n, err := s.Cleanup(context.Background(), now)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	after := read(t, path, Query{View: "stats"})
	if after.Total != before.Total || after.Commands != before.Commands || after.Players != before.Players || after.Victories != before.Victories || len(after.Days) != len(before.Days) {
		t.Fatal("pruning changed permanent stats")
	}
	if _, err := Read(context.Background(), path, Query{Session: "old-noise"}); err == nil {
		t.Fatal("old routine summary retained")
	}
	if v := read(t, path, Query{Session: "recent-noise"}); v.Selected.RetainedEvents != 2 {
		t.Fatal("recent detail pruned")
	}
	if v := read(t, path, Query{Session: "player"}); v.Selected.RetainedEvents != 4 {
		t.Fatal("promotion did not preserve early routine events")
	}
	v := read(t, path, Query{Session: "old-player"})
	if v.Selected.RetainedEvents != 0 || v.Selected.EventCount != 4 || v.Selected.Progress != 5 || len(v.Selected.Detail.Rewards) != 1 {
		t.Fatalf("expired player summary lost: %+v", v.Selected)
	}
	// Restart imports must not resurrect expired records or double-count traffic.
	var logs strings.Builder
	for _, e := range events {
		b, _ := json.Marshal(e)
		logs.Write(b)
		logs.WriteByte('\n')
	}
	os.WriteFile(logPath, []byte(logs.String()), 0600)
	if _, err := s.Import(context.Background(), logPath); err != nil {
		t.Fatal(err)
	}
	if v := read(t, path, Query{View: "stats"}); v.Total != before.Total || v.Victories != before.Victories {
		t.Fatal("retired logs were imported again")
	}
	if v := read(t, path, Query{Session: "old-player"}); v.Selected.RetainedEvents != 0 {
		t.Fatal("expired detail resurrected")
	}
}

func TestCursorBrowsingBeyondOldLimitAndBoundedSearch(t *testing.T) {
	s, path := openTest(t)
	now := time.Now().UTC()
	batch := make([]eventlog.Event, 0, 128)
	for i := 0; i < 2107; i++ {
		batch = append(batch, event(fmt.Sprintf("session-%04d", i), fmt.Sprintf("198.51.%d.%d", i/256, i%256), "move", "cargo", "east", now.Add(time.Duration(i/3)*time.Second)))
		if len(batch) == cap(batch) {
			record(t, s, batch)
			batch = batch[:0]
		}
	}
	record(t, s, batch)
	seen := map[string]bool{}
	q := Query{}
	pages := 0
	var previousFirst string
	for {
		v := read(t, path, q)
		pages++
		if len(v.Hosts) > 50 {
			t.Fatal("unbounded report")
		}
		for _, h := range v.Hosts {
			if seen[h.IP] {
				t.Fatal("duplicate cursor result")
			}
			seen[h.IP] = true
		}
		if pages == 2 {
			back := read(t, path, Query{After: v.Previous})
			if len(back.Hosts) != 50 || back.Hosts[0].IP != previousFirst {
				t.Fatal("newer cursor returned wrong page")
			}
		}
		if pages == 1 {
			previousFirst = v.Hosts[0].IP
		}
		if v.Next == "" {
			break
		}
		q.Before = v.Next
	}
	if pages != 43 || len(seen) != 2107 {
		t.Fatalf("lost old players: pages=%d hosts=%d", pages, len(seen))
	}
	v := read(t, path, Query{Search: "session-"})
	if !v.SearchLimited || len(v.Hosts) > 50 {
		t.Fatal("broad search was not bounded")
	}
	if v := read(t, path, Query{Search: "session-0000"}); len(v.Hosts) != 1 {
		t.Fatal("exact prefix lost old session")
	}
}

func TestCompleteEventPagesAndExpiryBatchBound(t *testing.T) {
	s, path := openTest(t)
	now := time.Now().UTC()
	var events []eventlog.Event
	for i := 0; i < 215; i++ {
		kind := "command"
		if i == 0 {
			kind = "move"
		}
		events = append(events, event("long-player", "192.0.2.3", kind, "cargo", fmt.Sprintf("command-%03d", i), now.Add(time.Duration(i)*time.Second)))
	}
	record(t, s, events)
	for page, length := range []int{100, 100, 15} {
		v := read(t, path, Query{Session: "long-player", EventPage: page + 1})
		if len(v.Selected.Events) != length || v.EventPages != 3 || v.Selected.RetainedEvents != 215 {
			t.Fatal("event pagination", page)
		}
	}
	events = nil
	for i := 0; i < 300; i++ {
		events = append(events, event(fmt.Sprintf("old-%03d", i), "192.0.2.1", "probe", "dock", "noise", now.Add(-8*24*time.Hour)))
	}
	record(t, s, events)
	n, err := s.Cleanup(context.Background(), now)
	if err != nil || n != 2*cleanupBatch {
		t.Fatal("cleanup is not bounded", n, err)
	}
	var count int
	s.db.QueryRow(`SELECT count(*) FROM events WHERE expires<?`, now.UnixNano()).Scan(&count)
	if count != 300-cleanupBatch {
		t.Fatal("cleanup swept entire backlog", count)
	}
}

func TestProgressSummaryStaysCompactOnRepetition(t *testing.T) {
	s, path := openTest(t)
	now := time.Now().UTC()
	var events []eventlog.Event
	for i := 0; i < 400; i++ {
		events = append(events, event("repeating", "192.0.2.1", "move", "cargo", "east", now.Add(time.Duration(i)*time.Second)))
	}
	record(t, s, events)
	v := read(t, path, Query{Session: "repeating"})
	if len(v.Selected.Detail.Rooms) != 1 || len(v.Selected.Detail.Route) != 64 || !v.Selected.Detail.RouteTruncated || v.Selected.EventCount != 400 {
		t.Fatal("repeated actions grew the summary or lost counts")
	}
	var terms int
	if err := s.db.QueryRow(`SELECT count(*) FROM search_terms WHERE session_id='repeating'`).Scan(&terms); err != nil {
		t.Fatal(err)
	}
	if terms > 10 {
		t.Fatal("repeated actions grew search index", terms)
	}
}

func TestHistorySurvivesRotationDeletionAndRestart(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	dbPath := Path(logPath)
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	l, err := eventlog.OpenRecorded(logPath, 4096, s)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 160; i++ {
		e := eventlog.Event{Session: fmt.Sprintf("visitor-%03d", i), IP: fmt.Sprintf("192.0.2.%d", i), Kind: "command", Text: strings.Repeat("x", 900)}
		l.Emit(e)
		e.Kind = "move"
		e.Room = "cargo"
		e.Text = "east"
		l.Emit(e)
	}
	l.Close()
	select {
	case err := <-l.Errors():
		t.Fatal(err)
	default:
	}
	s.Close()
	for _, path := range []string{logPath, logPath + ".1"} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v := read(t, dbPath, Query{Search: "visitor-000"})
	if v.Total != 160 || v.Commands != 160 || len(v.Hosts) != 1 || v.Hosts[0].Best.ID != "visitor-000" || l.Dropped() != 0 {
		t.Fatalf("old session lost: %+v", v)
	}
}

func TestImportCompleteLogsOnce(t *testing.T) {
	s, path := openTest(t)
	logPath := filepath.Join(t.TempDir(), "events.jsonl")
	var logs strings.Builder
	for i := 0; i < 2400; i++ {
		e := event(fmt.Sprintf("import-%04d", i), "192.0.2.8", "command", "dock", strings.Repeat("z", 900), time.Unix(int64(i+1), 0).UTC())
		b, _ := json.Marshal(e)
		logs.Write(b)
		logs.WriteByte('\n')
	}
	if err := os.WriteFile(logPath+".1", []byte(logs.String()+"invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	last := event("import-0000", "192.0.2.8", "victory", "vault", "root_console", time.Unix(4000, 0).UTC())
	b, _ := json.Marshal(last)
	os.WriteFile(logPath, append(b, '\n'), 0600)
	invalid, err := s.Import(context.Background(), logPath)
	if err != nil || invalid != 1 {
		t.Fatal(invalid, err)
	}
	invalid, err = s.Import(context.Background(), logPath)
	if err != nil || invalid != 0 {
		t.Fatal(invalid, err)
	}
	v := read(t, path, Query{})
	if v.Total != 2400 || v.Commands != 2400 || v.Victories != 1 || len(v.Hosts) != 1 || v.Hosts[0].Best.EventCount != 2 {
		t.Fatalf("import counts: %+v", v)
	}
}

func TestLegacyMigrationPreservesProgressAndCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	os.WriteFile(path, nil, 0600)
	s, err := open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 27, 23, 59, 59, 0, time.UTC)
	var events []eventlog.Event
	for i, kind := range []string{"connect", "command", "move", "victory"} {
		room := "dock"
		if i > 1 {
			room = "vault"
		}
		events = append(events, event("legacy", "192.0.2.1", kind, room, "root_console", now.Add(time.Duration(i)*time.Second)))
	}
	if _, err = s.db.Exec(`INSERT INTO sessions VALUES('legacy','192.0.2.1','ssh',2223,'vault','captain','vault recovered',?,?,1,1,0,4)`, now.UnixNano(), now.Add(3*time.Second).UnixNano()); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		b, _ := json.Marshal(e)
		hash := sha256.Sum256(b)
		if _, err = s.db.Exec(`INSERT INTO events(fingerprint,session_id,time,kind,text,payload) VALUES(?,?,?,?,?,?)`, hash[:], e.Session, e.Time.UnixNano(), e.Kind, e.Text, b); err != nil {
			t.Fatal(err)
		}
	}
	s.db.Exec(`INSERT INTO command_counts VALUES('random scanner input',1)`)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v := read(t, path, Query{})
	if v.Total != 1 || v.PlayerHosts != 1 || v.Commands != 1 || v.Victories != 1 || len(v.Hosts) != 1 || v.Hosts[0].Best.Progress != 5 || len(v.Hosts[0].Best.Detail.Rooms) != 2 {
		t.Fatalf("migration: %+v", v)
	}
	if v := read(t, path, Query{View: "stats"}); len(v.TopCommands) != 1 || v.TopCommands[0].Name != "other" {
		t.Fatal("unbounded command keys survived migration")
	}
	if v := read(t, path, Query{View: "stats"}); len(v.Days) != 2 || v.Days[0].Players != 1 || v.Days[0].Sessions != 0 || v.Days[0].Commands != 1 || v.Days[0].Victories != 1 || v.Days[1].Sessions != 1 {
		t.Fatalf("migration mixed UTC dates: %+v", v.Days)
	}
	if v := read(t, path, Query{Search: "captain"}); len(v.Hosts) != 1 {
		t.Fatal("legacy username missing from search")
	}
}

func TestReadOnlyConcurrentAndIndexedQueries(t *testing.T) {
	s, path := openTest(t)
	reader, err := open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.db.Exec(`DELETE FROM sessions`); err == nil {
		t.Fatal("reader can write")
	}
	reader.Close()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Go(func() {
			for j := 0; j < 4; j++ {
				if _, err := Read(context.Background(), path, Query{}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	for i := 0; i < 12; i++ {
		record(t, s, []eventlog.Event{event(fmt.Sprint(i), "192.0.2.1", "move", "cargo", "east", time.Now().Add(time.Duration(i)*time.Second))})
	}
	wg.Wait()
	for _, query := range []string{`SELECT id FROM sessions WHERE progress>0 AND (end,id)<(999999999999999999,'x') ORDER BY end DESC,id DESC LIMIT 51`, `SELECT session_id FROM search_terms WHERE term>='cargo' AND term<'cargp' LIMIT 501`, `SELECT id FROM events WHERE expires<1 ORDER BY expires LIMIT 128`} {
		rows, err := s.db.Query("EXPLAIN QUERY PLAN " + query)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, unused int
			var detail string
			rows.Scan(&id, &parent, &unused, &detail)
			plan.WriteString(detail)
		}
		rows.Close()
		if !strings.Contains(plan.String(), "SEARCH") || strings.Contains(plan.String(), "TEMP B-TREE") {
			t.Fatal("expensive query plan", plan.String())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Read(ctx, path, Query{}); err == nil {
		t.Fatal("ignored cancellation")
	}
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	if _, err := Read(context.Background(), missing, Query{}); err == nil {
		t.Fatal("missing accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read created database")
	}
}
