// Package adventures contains the editable starter adventure distributed with MudPot.
package adventures

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed default/*.json
var Files embed.FS

// Init creates a new adventure without overwriting an existing directory.
func Init(dir string) error {
	if err := os.Mkdir(dir, 0755); err != nil {
		return err
	}
	for _, name := range []string{"adventure.json", "rooms.json", "puzzles.json", "rewards.json"} {
		b, err := Files.ReadFile("default/" + name)
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}
