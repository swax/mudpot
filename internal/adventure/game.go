package adventure

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh"
)

type Emit func(kind, room, text string)
type Result struct {
	Text    string
	Quit    bool
	Victory bool
}
type Session struct {
	World       *World
	Room        string
	Inventory   map[string]bool
	unlocked    map[string]bool
	taken       map[string]bool
	puzzleDir   string
	elevated    bool
	completed   bool
	decoySSHKey string
	emit        Emit
}

func NewSession(w *World, emit Emit) *Session {
	if emit == nil {
		emit = func(string, string, string) {}
	}
	return &Session{World: w, Room: w.Start, Inventory: map[string]bool{}, unlocked: map[string]bool{}, taken: map[string]bool{}, emit: emit}
}
func Clean(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
}
func (s *Session) Greeting() string { return s.World.Title + "\n\n" + s.World.Intro + "\n" + s.Look() }
func (s *Session) Prompt() string {
	if s.puzzleDir != "" {
		return s.World.Puzzles[s.World.Rooms[s.Room].Exits[s.puzzleDir].Puzzle].Prompt
	}
	if s.elevated {
		return "root@" + s.hostname() + ":~# "
	}
	return "> "
}
func (s *Session) Look() string {
	r := s.World.Rooms[s.Room]
	text := "\n" + r.Name + "\n" + r.Description + "\n\nExits: " + strings.Join(Keys(r.Exits), ", ") + "\n"
	if len(r.Items) > 0 {
		text += "Items: " + strings.Join(Keys(r.Items), ", ") + "\n"
	}
	if len(r.Rewards) > 0 {
		text += "Recovery archive: " + strings.Join(r.Rewards, ", ") + "\n"
	}
	return text
}
func (s *Session) Run(raw string) Result {
	input := strings.ToLower(strings.TrimSpace(Clean(raw)))
	if input == "" {
		return Result{}
	}
	s.emit("command", s.Room, Clean(raw))
	if s.puzzleDir != "" {
		e := s.World.Rooms[s.Room].Exits[s.puzzleDir]
		p := s.World.Puzzles[e.Puzzle]
		if input == "cancel" || input == "back" {
			s.puzzleDir = ""
			return Result{Text: "You step back from the panel.\n"}
		}
		if input == "quit" || input == "exit" {
			return Result{Text: "Connection closed.\n", Quit: true}
		}
		for _, answer := range p.Answers {
			if Normalize(input) == Normalize(answer) {
				s.unlocked[s.Room+"/"+s.puzzleDir] = true
				s.puzzleDir = ""
				s.emit("puzzle", s.Room, e.Puzzle)
				return Result{Text: p.Success + "\n"}
			}
		}
		return Result{Text: p.Failure + "\n"}
	}
	parts := strings.Fields(input)
	cmd := parts[0]
	arg := strings.Trim(strings.Join(parts[1:], " "), "\"'")
	aliases := map[string]string{"n": "north", "s": "south", "e": "east", "w": "west", "u": "up", "d": "down"}
	if dir, ok := aliases[cmd]; ok {
		return s.move(dir)
	}
	if slices.Contains([]string{"north", "south", "east", "west", "up", "down"}, cmd) {
		return s.move(cmd)
	}
	switch cmd {
	case "help", "?":
		return Result{Text: "look | go <direction> | read <item> | take <item> | use <item>\ninventory | help | quit\nAt a challenge: enter the answer, or cancel.\nGoal: " + s.World.Intro + "\n"}
	case "look", "l":
		return Result{Text: s.Look()}
	case "go", "move", "walk":
		if d, ok := aliases[arg]; ok {
			arg = d
		}
		return s.move(arg)
	case "inventory", "inv", "i":
		return Result{Text: "Inventory: " + strings.Join(Keys(s.Inventory), ", ") + "\n"}
	case "read", "examine", "cat", "open", "check", "inspect":
		if arg == "" {
			return Result{Text: "Read what? Try look to list items.\n"}
		}
		if id := s.rewardID(arg); id != "" {
			return s.reward(id)
		}
		r := s.World.Rooms[s.Room]
		for _, key := range Keys(r.Items) {
			if matchItem(arg, key) {
				s.emit("read", s.Room, key)
				return Result{Text: r.Items[key] + "\n"}
			}
		}
		return Result{Text: "You don't see that here.\n"}
	case "take", "grab", "get", "pickup":
		if arg != "" {
			for _, key := range s.World.Rooms[s.Room].Pickup {
				if matchItem(arg, key) {
					if s.taken[key] {
						return Result{Text: "You already have the " + key + ".\n"}
					}
					s.Inventory[key] = true
					s.taken[key] = true
					s.emit("take", s.Room, key)
					return Result{Text: "You pick up the " + key + ".\n"}
				}
			}
		}
		return Result{Text: "You can't take that.\n"}
	case "use":
		if id := s.rewardID(arg); id != "" {
			return s.reward(id)
		}
		for _, dir := range Keys(s.World.Rooms[s.Room].Exits) {
			e := s.World.Rooms[s.Room].Exits[dir]
			if e.Key != "" && matchItem(arg, e.Key) && s.Inventory[e.Key] && !s.unlocked[s.Room+"/"+dir] {
				return s.unlock(dir, e)
			}
		}
		return Result{Text: "You can't use that here.\n"}
	case "quit", "exit", "logout", "q":
		return Result{Text: "You leave " + s.World.Title + ".\n", Quit: true}
	case "whoami":
		if s.elevated {
			return Result{Text: "root\n"}
		}
		return Result{Text: "visitor // administrator recovery required\n"}
	case "id":
		if s.elevated {
			return Result{Text: "uid=0(root) gid=0(root) groups=0(root)\n"}
		}
		return Result{Text: "uid=1000(visitor) // administrator recovery required\n"}
	case "pwd":
		if s.elevated {
			return Result{Text: "/root\n"}
		}
		return Result{Text: "/adventure/" + s.Room + "\n"}
	case "ls":
		return Result{Text: s.Look()}
	case "hostname":
		return Result{Text: s.hostname() + "\n"}
	case "uname":
		return Result{Text: "Linux " + s.hostname() + " 6.12.0 recovery x86_64\n"}
	default:
		if response := s.World.Responses[cmd]; response != "" {
			return Result{Text: response + "\n"}
		}
		return Result{Text: fmt.Sprintf("Unknown command %q. Type help.\n", cmd)}
	}
}
func (s *Session) hostname() string {
	if s.World.Hostname != "" {
		return s.World.Hostname
	}
	return "recovery"
}

func matchItem(arg, key string) bool {
	return arg != "" && (arg == key || strings.ReplaceAll(arg, " ", "_") == key)
}
func (s *Session) unlock(dir string, e Exit) Result {
	if e.Puzzle != "" {
		s.puzzleDir = dir
		return Result{Text: e.Message + "\n"}
	}
	s.unlocked[s.Room+"/"+dir] = true
	return Result{Text: e.Message + "\n"}
}
func (s *Session) move(dir string) Result {
	e, ok := s.World.Rooms[s.Room].Exits[dir]
	if !ok {
		return Result{Text: "You can't go that way.\n"}
	}
	message := ""
	if !s.unlocked[s.Room+"/"+dir] && (e.Key != "" || e.Puzzle != "") {
		if e.Key != "" && !s.Inventory[e.Key] {
			return Result{Text: "The way is locked. You need " + e.Key + ".\n"}
		}
		r := s.unlock(dir, e)
		if s.puzzleDir != "" {
			return r
		}
		message = r.Text
	}
	s.Room = e.To
	s.emit("move", s.Room, dir)
	return Result{Text: message + s.Look()}
}
func (s *Session) rewardID(arg string) string {
	for _, id := range s.World.Rooms[s.Room].Rewards {
		if arg == id || slices.Contains(s.World.Rewards[id].Aliases, arg) {
			return id
		}
	}
	return ""
}
func (s *Session) reward(id string) Result {
	r := s.World.Rewards[id]
	content := r.Content
	if strings.Contains(content, "{{decoy_ssh_private_key}}") {
		if s.decoySSHKey == "" {
			_, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return Result{Text: "Recovery media unavailable.\n"}
			}
			block, err := ssh.MarshalPrivateKey(key, "station-recovery@station.invalid")
			if err != nil {
				return Result{Text: "Recovery media unavailable.\n"}
			}
			s.decoySSHKey = string(pem.EncodeToMemory(block))
		}
		content = strings.ReplaceAll(content, "{{decoy_ssh_private_key}}", s.decoySSHKey)
	}
	if r.Kind == "shell" {
		s.elevated = true
	}
	text := content + "\n"
	victory := !s.completed
	if victory {
		s.completed = true
		s.emit("victory", s.Room, id)
		text += "\n" + s.World.Victory + "\n"
	} else {
		s.emit("reward", s.Room, id)
	}
	return Result{Text: text, Victory: victory}
}
