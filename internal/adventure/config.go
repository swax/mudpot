package adventure

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/swax/mudpot/adventures"
)

type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	Title         string            `json:"title"`
	Hostname      string            `json:"hostname,omitempty"`
	Start         string            `json:"start"`
	Intro         string            `json:"intro"`
	Victory       string            `json:"victory"`
	Responses     map[string]string `json:"responses"`
}
type Exit struct {
	To      string `json:"to"`
	Key     string `json:"key,omitempty"`
	Puzzle  string `json:"puzzle,omitempty"`
	Message string `json:"message,omitempty"`
}
type Room struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Exits       map[string]Exit   `json:"exits"`
	Items       map[string]string `json:"items"`
	Pickup      []string          `json:"pickup,omitempty"`
	Rewards     []string          `json:"rewards,omitempty"`
}
type Puzzle struct {
	Prompt  string   `json:"prompt"`
	Answers []string `json:"answers"`
	Success string   `json:"success"`
	Failure string   `json:"failure"`
}
type Reward struct {
	Kind    string   `json:"kind"`
	Aliases []string `json:"aliases,omitempty"`
	Content string   `json:"content"`
}
type World struct {
	Manifest
	Rooms   map[string]Room
	Puzzles map[string]Puzzle
	Rewards map[string]Reward
}

const maxFile = 512 * 1024

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// Load reads only four fixed filenames. Configuration never executes code or reads reward paths.
func Load(dir string) (*World, error) {
	var source fs.FS
	if dir == "" {
		var err error
		source, err = fs.Sub(adventures.Files, "default")
		if err != nil {
			return nil, err
		}
	} else {
		source = os.DirFS(dir)
	}
	w := &World{}
	for _, file := range []struct {
		name string
		dest any
	}{
		{"adventure.json", &w.Manifest}, {"rooms.json", &w.Rooms},
		{"puzzles.json", &w.Puzzles}, {"rewards.json", &w.Rewards},
	} {
		f, err := source.Open(file.name)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
		f.Close()
		if err != nil {
			return nil, err
		}
		if len(b) > maxFile {
			return nil, fmt.Errorf("%s exceeds 512 KiB", file.name)
		}
		d := json.NewDecoder(strings.NewReader(string(b)))
		d.DisallowUnknownFields()
		if err = d.Decode(file.dest); err != nil {
			return nil, fmt.Errorf("%s: %w", file.name, err)
		}
		if err = d.Decode(new(any)); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s: trailing JSON", file.name)
		}
		for _, r := range string(b) {
			if r == 0 {
				return nil, fmt.Errorf("%s: NUL is not allowed", file.name)
			}
		}
	}
	if err := w.Validate(); err != nil {
		return nil, err
	}
	return w, nil
}

func textOK(s string) bool {
	if len(s) > 32768 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
func Normalize(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), "") }
func Keys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (w *World) Validate() error {
	if w.Hostname != "" && !identifier.MatchString(w.Hostname) {
		return errors.New("hostname must be a lowercase identifier without spaces")
	}
	if w.SchemaVersion != 1 {
		return errors.New("schema_version must be 1")
	}
	if w.Title == "" || w.Intro == "" || w.Victory == "" {
		return errors.New("title, intro, and victory are required")
	}
	if len(w.Rooms) == 0 || len(w.Rooms) > 256 || len(w.Puzzles) > 256 || len(w.Rewards) == 0 || len(w.Rewards) > 256 {
		return errors.New("use 1-256 rooms and rewards, and at most 256 puzzles")
	}
	if _, ok := w.Rooms[w.Start]; !ok {
		return fmt.Errorf("unknown start room %q", w.Start)
	}
	texts := []string{w.Title, w.Intro, w.Victory}
	pickups := map[string]bool{}
	for id, room := range w.Rooms {
		if !identifier.MatchString(id) || room.Name == "" || room.Description == "" {
			return fmt.Errorf("room %q needs a valid id, name, and description", id)
		}
		texts = append(texts, room.Name, room.Description)
		for item, content := range room.Items {
			if !identifier.MatchString(item) {
				return fmt.Errorf("invalid item %q", item)
			}
			texts = append(texts, content)
		}
		for _, item := range room.Pickup {
			if _, ok := room.Items[item]; !ok {
				return fmt.Errorf("room %s: pickup %s needs an item description", id, item)
			}
			pickups[item] = true
		}
		for _, reward := range room.Rewards {
			if _, ok := w.Rewards[reward]; !ok {
				return fmt.Errorf("room %s: unknown reward %s", id, reward)
			}
		}
	}
	for id, room := range w.Rooms {
		for dir, exit := range room.Exits {
			if !identifier.MatchString(dir) {
				return fmt.Errorf("room %s: invalid direction %q", id, dir)
			}
			if _, ok := w.Rooms[exit.To]; !ok {
				return fmt.Errorf("room %s: unknown destination %s", id, exit.To)
			}
			if exit.Key != "" && !pickups[exit.Key] {
				return fmt.Errorf("room %s: unknown key %s", id, exit.Key)
			}
			if exit.Puzzle != "" {
				if _, ok := w.Puzzles[exit.Puzzle]; !ok {
					return fmt.Errorf("room %s: unknown puzzle %s", id, exit.Puzzle)
				}
			}
			texts = append(texts, exit.Message)
		}
	}
	for id, p := range w.Puzzles {
		if !identifier.MatchString(id) || p.Prompt == "" || p.Success == "" || p.Failure == "" || len(p.Answers) == 0 || len(p.Answers) > 32 {
			return fmt.Errorf("invalid puzzle %q", id)
		}
		texts = append(texts, p.Prompt, p.Success, p.Failure)
		for _, answer := range p.Answers {
			if Normalize(answer) == "" || len(answer) > 256 {
				return fmt.Errorf("puzzle %s: answer must be 1-256 bytes", id)
			}
			texts = append(texts, answer)
		}
	}
	aliases := map[string]bool{}
	for id := range w.Rewards {
		aliases[id] = true
	}
	for id, r := range w.Rewards {
		if !identifier.MatchString(id) || (r.Kind != "file" && r.Kind != "shell") || r.Content == "" {
			return fmt.Errorf("invalid reward %q", id)
		}
		texts = append(texts, r.Content)
		for _, alias := range r.Aliases {
			if alias == "" || alias != strings.ToLower(alias) || strings.ContainsAny(alias, " \t\r\n") || len(alias) > 256 || aliases[alias] {
				return fmt.Errorf("reward %s: invalid or duplicate alias %q", id, alias)
			}
			aliases[alias] = true
			texts = append(texts, alias)
		}
	}
	for cmd, response := range w.Responses {
		if cmd == "" || len(cmd) > 256 || cmd != strings.ToLower(cmd) || len(strings.Fields(cmd)) != 1 || strings.TrimSpace(cmd) != cmd || !textOK(cmd) {
			return fmt.Errorf("invalid response command %q", cmd)
		}
		texts = append(texts, response)
	}
	for _, value := range texts {
		if !textOK(value) {
			return errors.New("text must be at most 32 KiB, without terminal control characters")
		}
	}
	// Find rooms reachable with obtainable keys. Puzzles are assumed solvable by their configured answers.
	reached, inventory := map[string]bool{w.Start: true}, map[string]bool{}
	for changed := true; changed; {
		changed = false
		for id := range reached {
			for _, item := range w.Rooms[id].Pickup {
				if !inventory[item] {
					inventory[item] = true
					changed = true
				}
			}
			for _, exit := range w.Rooms[id].Exits {
				if !reached[exit.To] && (exit.Key == "" || inventory[exit.Key]) {
					reached[exit.To] = true
					changed = true
				}
			}
		}
	}
	usedRewards := map[string]bool{}
	for id, room := range w.Rooms {
		if !reached[id] {
			return fmt.Errorf("room %s is unreachable with obtainable keys", id)
		}
		for _, reward := range room.Rewards {
			usedRewards[reward] = true
		}
	}
	for id := range w.Rewards {
		if !usedRewards[id] {
			return fmt.Errorf("reward %s is not placed in a room", id)
		}
	}
	return nil
}
