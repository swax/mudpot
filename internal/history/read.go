package history

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/swax/mudpot/internal/eventlog"
)

var ErrQuery = errors.New("invalid history query")

type cursor struct {
	Time int64
	ID   string
}

func Cursor(t time.Time, id string) string {
	b, _ := json.Marshal(cursor{t.UnixNano(), id})
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(text string) (cursor, error) {
	var c cursor
	b, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || len(b) > 256 {
		return c, ErrQuery
	}
	if json.Unmarshal(b, &c) != nil || c.ID == "" || len(c.ID) > 128 {
		return c, ErrQuery
	}
	return c, nil
}
func Validate(q Query) error {
	if len(q.Search) > 256 || len(q.Session) > 64 || len(q.Host) > 64 {
		return ErrQuery
	}
	if q.View != "" && q.View != "players" && q.View != "sessions" && q.View != "recent" && q.View != "stats" {
		return ErrQuery
	}
	if q.Activity != "" && q.Activity != "played" && q.Activity != "victory" {
		return ErrQuery
	}
	if q.Before != "" && q.After != "" {
		return ErrQuery
	}
	for _, text := range []string{q.Before, q.After} {
		if text != "" {
			if _, err := decodeCursor(text); err != nil {
				return err
			}
		}
	}
	return nil
}

func Read(ctx context.Context, path string, q Query) (*View, error) {
	if err := Validate(q); err != nil {
		return nil, err
	}
	if q.View == "" {
		q.View = "players"
	}
	s, err := open(path, true)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	v := &View{Query: q, Updated: time.Now().UTC()}
	if err = tx.QueryRowContext(ctx, `SELECT sessions,hosts,commands,players,victories,limited,player_hosts FROM totals WHERE id=1`).Scan(&v.Total, &v.Unique, &v.Commands, &v.Players, &v.Victories, &v.Limited, &v.PlayerHosts); err != nil {
		return nil, err
	}
	if q.Session != "" {
		v.Selected, err = scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions s WHERE id=?`, q.Session))
		if err != nil {
			return nil, err
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE session_id=?`, q.Session).Scan(&v.Selected.RetainedEvents); err != nil {
			return nil, err
		}
		v.EventPages = max(1, (v.Selected.RetainedEvents+EventPageSize-1)/EventPageSize)
		v.EventPage = min(max(q.EventPage, 1), v.EventPages)
		rows, e := tx.QueryContext(ctx, `SELECT payload FROM events WHERE session_id=? ORDER BY time,id LIMIT ? OFFSET ?`, q.Session, EventPageSize, (v.EventPage-1)*EventPageSize)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var b []byte
			var event eventlog.Event
			if err = rows.Scan(&b); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal(b, &event); err != nil {
				rows.Close()
				return nil, err
			}
			v.Selected.Events = append(v.Selected.Events, event)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	} else if q.View != "stats" {
		where := " WHERE "
		var args []any
		hosts := q.View == "players" && q.Host == ""
		if hosts {
			where += "h.plays>0"
			if q.Activity == "victory" {
				where = " WHERE h.victories>0"
			}
		} else {
			where += "s.progress>0"
			if q.View == "recent" {
				where = " WHERE 1=1"
			}
			if q.Activity == "victory" {
				where = " WHERE s.victories>0"
			}
			if q.Host != "" {
				where += " AND s.ip=?"
				args = append(args, q.Host)
			}
		}
		if q.Search != "" {
			// Prefix lookup touches at most 501 index entries. It never scans raw
			// event text or counts/sorts every historical match.
			prefix := strings.ToLower(strings.TrimSpace(q.Search))
			rows, e := tx.QueryContext(ctx, `SELECT session_id FROM search_terms WHERE term>=? AND term<? LIMIT 501`, prefix, prefix+"\U0010ffff")
			if e != nil {
				return nil, e
			}
			var ids []any
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return nil, err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			if len(ids) > 500 {
				v.SearchLimited = true
				ids = ids[:500]
			}
			if len(ids) == 0 {
				where += " AND 0=1"
			} else {
				marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
				if hosts {
					where += " AND h.ip IN(SELECT ip FROM sessions WHERE id IN(" + marks + "))"
				} else {
					where += " AND s.id IN(" + marks + ")"
				}
				args = append(args, ids...)
			}
		}
		field, key := "s.end", "s.id"
		if hosts {
			field, key = "h.last_play", "h.ip"
		}
		order := " DESC"
		if q.Before != "" || q.After != "" {
			text, op := q.Before, "<"
			if q.After != "" {
				text, op, order = q.After, ">", " ASC"
			}
			c, _ := decodeCursor(text)
			where += " AND (" + field + "," + key + ")" + op + "(?,?)"
			args = append(args, c.Time, c.ID)
		}
		if hosts {
			rows, e := tx.QueryContext(ctx, `SELECT h.ip,h.connections,h.plays,h.victories,h.first_play,h.last_play,`+sessionColumns+` FROM hosts h JOIN sessions s ON s.id=h.best_session`+where+` ORDER BY h.last_play`+order+`,h.ip`+order+` LIMIT 51`, args...)
			if e != nil {
				return nil, e
			}
			for rows.Next() {
				h := &Host{}
				var first, last int64
				h.Best, err = scanSession(rows, &h.IP, &h.Connections, &h.Plays, &h.Victories, &first, &last)
				if err != nil {
					rows.Close()
					return nil, err
				}
				h.First, h.Last = at(first), at(last)
				v.Hosts = append(v.Hosts, h)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			hasMore := len(v.Hosts) > PageSize
			if hasMore {
				v.Hosts = v.Hosts[:PageSize]
			}
			if q.After != "" {
				slices.Reverse(v.Hosts)
			}
			if len(v.Hosts) > 0 {
				first, last := v.Hosts[0], v.Hosts[len(v.Hosts)-1]
				if (q.After == "" && hasMore) || q.After != "" {
					v.Next = Cursor(last.Last, last.IP)
				}
				if q.Before != "" || (q.After != "" && hasMore) {
					v.Previous = Cursor(first.Last, first.IP)
				}
			}
		} else {
			rows, e := tx.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions s`+where+` ORDER BY s.end`+order+`,s.id`+order+` LIMIT 51`, args...)
			if e != nil {
				return nil, e
			}
			for rows.Next() {
				entry, e := scanSession(rows)
				if e != nil {
					rows.Close()
					return nil, e
				}
				v.Sessions = append(v.Sessions, entry)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			hasMore := len(v.Sessions) > PageSize
			if hasMore {
				v.Sessions = v.Sessions[:PageSize]
			}
			if q.After != "" {
				slices.Reverse(v.Sessions)
			}
			if len(v.Sessions) > 0 {
				first, last := v.Sessions[0], v.Sessions[len(v.Sessions)-1]
				if (q.After == "" && hasMore) || q.After != "" {
					v.Next = Cursor(last.End, last.ID)
				}
				if q.Before != "" || (q.After != "" && hasMore) {
					v.Previous = Cursor(first.End, first.ID)
				}
			}
		}
	}
	if q.View == "stats" {
		for _, item := range []struct {
			query string
			dest  *[]Count
		}{{`SELECT name,total FROM room_counts ORDER BY total DESC,name LIMIT 256`, &v.Rooms}, {`SELECT name,total FROM command_counts ORDER BY total DESC,name LIMIT 15`, &v.TopCommands}} {
			rows, e := tx.QueryContext(ctx, item.query)
			if e != nil {
				return nil, e
			}
			for rows.Next() {
				var c Count
				if err = rows.Scan(&c.Name, &c.Value); err != nil {
					rows.Close()
					return nil, err
				}
				*item.dest = append(*item.dest, c)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
		}
		rows, e := tx.QueryContext(ctx, `SELECT day,sessions,players,commands,victories,limited FROM daily ORDER BY day DESC LIMIT 30`)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var d Daily
			if err = rows.Scan(&d.Day, &d.Sessions, &d.Players, &d.Commands, &d.Victories, &d.Limited); err != nil {
				rows.Close()
				return nil, err
			}
			v.Days = append(v.Days, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return v, tx.Commit()
}
