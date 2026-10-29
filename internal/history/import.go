package history

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/swax/mudpot/internal/eventlog"
)

// Import reads all surviving records, including records outside the old dashboard
// sample. Run before opening the log writer, so files cannot rotate underneath it.
func (s *Store) Import(ctx context.Context, logPath string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var done int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM metadata WHERE key='logs_imported'").Scan(&done); err != nil {
		return 0, err
	}
	if done > 0 {
		return 0, nil
	}
	invalid := 0
	for _, path := range []string{logPath + ".1", logPath} {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return invalid, err
		}
		err = func() error {
			defer f.Close()
			info, err := f.Stat()
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("invalid event log %s", path)
			}
			scanner := bufio.NewScanner(f)
			scanner.Buffer(make([]byte, 4096), 1024*1024)
			batch := make([]eventlog.Event, 0, 128)
			for scanner.Scan() {
				if err := ctx.Err(); err != nil {
					return err
				}
				var e eventlog.Event
				if json.Unmarshal(scanner.Bytes(), &e) != nil || e.Time.IsZero() || e.Kind == "" {
					invalid++
					continue
				}
				batch = append(batch, e)
				if len(batch) == cap(batch) {
					if err := s.record(ctx, batch); err != nil {
						return err
					}
					batch = batch[:0]
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
			return s.record(ctx, batch)
		}()
		if err != nil {
			return invalid, err
		}
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES('logs_imported','1')")
	return invalid, err
}
