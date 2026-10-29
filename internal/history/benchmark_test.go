package history

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/swax/mudpot/internal/eventlog"
)

// Compare report work with and without a large routine-connection population.
// Setup is outside the timed region; no transcripts are loaded by these views.
func BenchmarkReports(b *testing.B) {
	for _, noise := range []int{0, 100000} {
		path := filepath.Join(b.TempDir(), "history.sqlite")
		s, pathErr := Open(path)
		if pathErr != nil {
			b.Fatal(pathErr)
		}
		var batch []eventlog.Event
		for i := 0; i < 500; i++ {
			batch = append(batch, eventlog.Event{Time: time.Now().Add(time.Duration(i) * time.Second), Session: fmt.Sprintf("player-%04d", i), IP: fmt.Sprintf("192.0.%d.%d", i/256, i%256), Kind: "move", Room: "cargo"})
		}
		if err := s.Record(batch); err != nil {
			b.Fatal(err)
		}
		if noise > 0 {
			_, err := s.db.Exec(`WITH RECURSIVE n(x) AS(VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<?)
 INSERT INTO sessions(id,ip,protocol,port,room,user,status,start,end,commands,victories,limited,event_count)
 SELECT 'noise-'||x,'198.51.100.1','tcp',2222,'dock','','disconnected',x,x,0,0,0,1 FROM n`, noise)
			if err != nil {
				b.Fatal(err)
			}
		}
		for _, view := range []string{"players", "stats"} {
			b.Run(fmt.Sprintf("%s/noise-%d", view, noise), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := Read(context.Background(), path, Query{View: view}); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
		s.Close()
	}
}
