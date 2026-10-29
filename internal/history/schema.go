package history

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/swax/mudpot/internal/eventlog"
)

const legacySchema = `CREATE TABLE IF NOT EXISTS sessions (
 id TEXT PRIMARY KEY, ip TEXT NOT NULL, protocol TEXT NOT NULL, port INTEGER NOT NULL,
 room TEXT NOT NULL, user TEXT NOT NULL, status TEXT NOT NULL, start INTEGER NOT NULL,
 end INTEGER NOT NULL, commands INTEGER NOT NULL DEFAULT 0, victories INTEGER NOT NULL DEFAULT 0,
 limited INTEGER NOT NULL DEFAULT 0, event_count INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY, fingerprint BLOB NOT NULL UNIQUE,
 session_id TEXT NOT NULL,time INTEGER NOT NULL,kind TEXT NOT NULL,text TEXT NOT NULL,payload BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS events_session ON events(session_id,time,id);
CREATE TABLE IF NOT EXISTS room_counts(name TEXT PRIMARY KEY,total INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS command_counts(name TEXT PRIMARY KEY,total INTEGER NOT NULL);`

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL;`); err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version == 2 {
		return nil
	}
	if version > 2 {
		return fmt.Errorf("session history schema %d is newer than this binary", version)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, legacySchema+`
ALTER TABLE sessions ADD COLUMN progress INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN summary BLOB NOT NULL DEFAULT '{}';
ALTER TABLE events ADD COLUMN expires INTEGER NOT NULL DEFAULT 0;
DROP INDEX IF EXISTS sessions_recent;
CREATE INDEX sessions_recent ON sessions(end DESC,id DESC);
CREATE INDEX IF NOT EXISTS sessions_played ON sessions(end DESC,id DESC) WHERE progress>0;
CREATE INDEX IF NOT EXISTS sessions_victory ON sessions(end DESC,id DESC) WHERE victories>0;
CREATE INDEX IF NOT EXISTS sessions_host ON sessions(ip,end DESC,id DESC) WHERE progress>0;
CREATE INDEX IF NOT EXISTS sessions_expire ON sessions(end) WHERE progress=0;
CREATE INDEX IF NOT EXISTS events_expire ON events(expires);
CREATE TABLE totals(id INTEGER PRIMARY KEY CHECK(id=1),sessions INTEGER NOT NULL,hosts INTEGER NOT NULL,
 commands INTEGER NOT NULL,players INTEGER NOT NULL,victories INTEGER NOT NULL,limited INTEGER NOT NULL,player_hosts INTEGER NOT NULL);
INSERT INTO totals VALUES(1,0,0,0,0,0,0,0);
CREATE TABLE daily(day TEXT PRIMARY KEY,sessions INTEGER NOT NULL,players INTEGER NOT NULL,commands INTEGER NOT NULL,victories INTEGER NOT NULL,limited INTEGER NOT NULL);
CREATE TABLE hosts(ip TEXT PRIMARY KEY,connections INTEGER NOT NULL,plays INTEGER NOT NULL,victories INTEGER NOT NULL,
 first_play INTEGER NOT NULL,last_play INTEGER NOT NULL,best_score INTEGER NOT NULL,best_session TEXT NOT NULL);
CREATE INDEX hosts_played ON hosts(last_play DESC,ip DESC) WHERE plays>0;
CREATE INDEX hosts_victory ON hosts(last_play DESC,ip DESC) WHERE victories>0;
CREATE TABLE search_terms(term TEXT NOT NULL,session_id TEXT NOT NULL,PRIMARY KEY(term,session_id)) WITHOUT ROWID;
CREATE INDEX search_session ON search_terms(session_id);
CREATE TABLE metadata(key TEXT PRIMARY KEY,value TEXT NOT NULL);
`); err != nil {
		return err
	}
	// Only the first upgrade scans legacy detail. Summaries are built before any
	// pruning, in session order so memory is independent of the database size.
	rows, err := tx.QueryContext(ctx, `SELECT session_id,payload FROM events ORDER BY session_id,time,id`)
	if err != nil {
		return err
	}
	var current *Session
	flush := func() error {
		if current == nil {
			return nil
		}
		b, err := json.Marshal(current.Detail)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE sessions SET progress=?,summary=? WHERE id=?`, current.Progress, b, current.ID); err != nil {
			return err
		}
		if current.Progress > 0 {
			return writeTerms(ctx, tx, current)
		}
		return nil
	}
	for rows.Next() {
		var id string
		var payload []byte
		if err = rows.Scan(&id, &payload); err != nil {
			rows.Close()
			return err
		}
		if current == nil || current.ID != id {
			if err = flush(); err != nil {
				rows.Close()
				return err
			}
			current, err = scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions s WHERE id=?`, id))
			if err != nil {
				rows.Close()
				return err
			}
		}
		var e eventlog.Event
		if err = json.Unmarshal(payload, &e); err != nil {
			rows.Close()
			return err
		}
		current.apply(e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = flush(); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO hosts SELECT ip,count(*),sum(progress>0),sum(victories),
 coalesce(min(CASE WHEN progress>0 THEN start END),0),coalesce(max(CASE WHEN progress>0 THEN end END),0),0,'' FROM sessions WHERE ip<>'' GROUP BY ip;
UPDATE totals SET sessions=(SELECT count(*) FROM sessions),hosts=(SELECT count(*) FROM hosts),
 commands=(SELECT coalesce(sum(commands),0) FROM sessions),players=(SELECT count(*) FROM sessions WHERE progress>0),
 victories=(SELECT coalesce(sum(victories),0) FROM sessions),limited=(SELECT coalesce(sum(limited),0) FROM sessions),
 player_hosts=(SELECT count(*) FROM hosts WHERE plays>0) WHERE id=1;
INSERT INTO daily SELECT strftime('%Y-%m-%d',start/1000000000,'unixepoch'),count(*),0,0,0,0 FROM sessions GROUP BY 1;
INSERT INTO daily SELECT strftime('%Y-%m-%d',time/1000000000,'unixepoch'),0,0,sum(kind='command'),sum(kind='victory'),sum(kind='limit') FROM events GROUP BY 1
 ON CONFLICT(day) DO UPDATE SET commands=excluded.commands,victories=excluded.victories,limited=excluded.limited;
INSERT INTO daily SELECT strftime('%Y-%m-%d',first_play/1000000000,'unixepoch'),0,count(*),0,0,0 FROM
 (SELECT min(time) AS first_play FROM events WHERE kind IN('read','move','take','puzzle','victory','reward') GROUP BY session_id) GROUP BY 1
 ON CONFLICT(day) DO UPDATE SET players=excluded.players;
UPDATE events SET expires=time+CASE WHEN (SELECT progress FROM sessions WHERE id=events.session_id)>0 THEN ? ELSE ? END;`, int64(GameplayRetention), int64(RoutineRetention)); err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions s WHERE progress>0`)
	if err != nil {
		return err
	}
	for rows.Next() {
		entry, e := scanSession(rows)
		if e != nil {
			rows.Close()
			return e
		}
		if err = updateHost(ctx, tx, entry, 0, 0, 0); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Replace arbitrary scanner text keys with a fixed set of categories.
	counts := map[string]int{}
	rows, err = tx.QueryContext(ctx, `SELECT name,total FROM command_counts`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var text string
		var total int
		if err = rows.Scan(&text, &total); err != nil {
			rows.Close()
			return err
		}
		counts[commandCategory(text)] += total
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM command_counts`); err != nil {
		return err
	}
	for name, total := range counts {
		if _, err = tx.ExecContext(ctx, `INSERT INTO command_counts VALUES(?,?)`, name, total); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `PRAGMA user_version=2`); err != nil {
		return err
	}
	return tx.Commit()
}

// Time conversion is centralized so expiration comparisons use UTC nanoseconds.
func at(n int64) time.Time { return time.Unix(0, n).UTC() }
