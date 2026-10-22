package adventure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/swax/mudpot/adventures"
)

var walkthrough = []string{"east", "take access_crystal", "west", "north", "west", "take security_seal", "use access_crystal", "north", "east", "use security_seal", "the one", "north"}

func defaultWorld(t testing.TB) *World {
	t.Helper()
	w, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func TestVictoryAndSimulatedAccess(t *testing.T) {
	w := defaultWorld(t)
	wins := 0
	s := NewSession(w, func(kind, room, text string) {
		if kind == "victory" {
			wins++
		}
	})
	for _, word := range []string{"ROOT SYSTEM ACCESS", "SSH PRIVATE KEY", "credentials"} {
		if !strings.Contains(s.Greeting(), word) {
			t.Fatalf("intro missing %s", word)
		}
	}
	for _, input := range []string{"cat /root/.ssh/id_ed25519", "use root_console", "read credentials.env"} {
		if result := s.Run(input); result.Victory || strings.Contains(result.Text, "PRIVATE KEY-----") {
			t.Fatalf("reward bypass: %s", input)
		}
	}
	if !strings.Contains(s.Run("north").Text, "Lower Passage") {
		t.Fatal("movement")
	}
	s.Run("west")
	if !strings.Contains(s.Run("north").Text, "locked") {
		t.Fatal("door bypass")
	}
	s = NewSession(w, func(kind, room, text string) {
		if kind == "victory" {
			wins++
		}
	})
	for _, input := range walkthrough {
		result := s.Run(input)
		if result.Quit || result.Victory {
			t.Fatalf("unexpected early completion at %s", input)
		}
	}
	if s.Room != "vault" {
		t.Fatal("not at vault:", s.Room)
	}
	result := s.Run("use root_console")
	if !result.Victory || result.Quit || !strings.Contains(result.Text, "Administrator access granted") {
		t.Fatal(result)
	}
	if s.Run("whoami").Text != "root\n" {
		t.Fatal("no simulated root access")
	}
	for _, input := range []string{"cat /root/.ssh/id_ed25519", "cat /opt/station/.env", "read backups.txt"} {
		result = s.Run(input)
		if result.Quit || strings.Contains(result.Text, "don't see") {
			t.Fatal(input, result)
		}
	}
	if wins != 1 {
		t.Fatalf("victory counted %d times", wins)
	}
	if !strings.Contains(s.Run("cat /etc/passwd").Text, "don't see") {
		t.Fatal("host file access")
	}
	if !strings.Contains(s.Run("curl https://example.com").Text, "offline") {
		t.Fatal("external command execution")
	}
	if !s.Run("quit").Quit {
		t.Fatal("quit")
	}
}
func TestSessionIsolationAndPuzzleCancel(t *testing.T) {
	w := defaultWorld(t)
	a, b := NewSession(w, nil), NewSession(w, nil)
	for _, input := range walkthrough[:10] {
		a.Run(input)
	}
	if !strings.Contains(a.Prompt(), "We live for") {
		t.Fatal("puzzle not active")
	}
	if !strings.Contains(a.Run("valen").Text, "not the answer") {
		t.Fatal("wrong answer accepted")
	}
	a.Run("cancel")
	if a.Prompt() != "> " {
		t.Fatal("cancel did not reset")
	}
	a.Run("north")
	a.Run("THE ONE")
	a.Run("north")
	if a.Room != "vault" || b.Room != w.Start || len(b.Inventory) != 0 {
		t.Fatal("session isolation")
	}
	if w.Rooms["vault_door"].Exits["north"].Puzzle != "the_one" {
		t.Fatal("world mutated")
	}
	a.Run("south")
	if strings.Contains(a.Run("use security_seal").Text, "activates") {
		t.Fatal("repeat unlock")
	}
}

func TestDecoyKeysAreValidStableAndSessionSpecific(t *testing.T) {
	w := defaultWorld(t)
	a, b := NewSession(w, nil), NewSession(w, nil)
	a.Room = "vault"
	b.Room = "vault"
	a.Run("use root_console")
	b.Run("use root_console")
	first := a.Run("read id_ed25519").Text
	if first != a.Run("read id_ed25519").Text {
		t.Fatal("key changes within session")
	}
	if first == b.Run("read id_ed25519").Text {
		t.Fatal("key reused between sessions")
	}
	if _, err := ssh.ParsePrivateKey([]byte(first)); err != nil {
		t.Fatal("invalid decoy", err)
	}
}
func TestConfigCloneValidationAndNoOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom")
	if err := adventures.Init(dir); err != nil {
		t.Fatal(err)
	}
	if err := adventures.Init(dir); err == nil {
		t.Fatal("overwrote adventure")
	}
	w, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	w.Title = "My Adventure"
	b, _ := json.Marshal(w.Manifest)
	if err := os.WriteFile(filepath.Join(dir, "adventure.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
	w, err = Load(dir)
	if err != nil || w.Title != "My Adventure" {
		t.Fatal("custom adventure", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "adventure.json"), []byte(`{"schema_version":1,"typo":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestInvalidWorld(t *testing.T) {
	cases := map[string]func(*World){
		"missing start":       func(w *World) { w.Start = "missing" },
		"missing destination": func(w *World) { w.Rooms["lobby"].Exits["north"] = Exit{To: "missing"} },
		"unknown key":         func(w *World) { w.Rooms["lobby"].Exits["north"] = Exit{To: "corridor", Key: "missing"} },
		"unknown puzzle":      func(w *World) { w.Rooms["lobby"].Exits["north"] = Exit{To: "corridor", Puzzle: "missing"} },
		"terminal escape":     func(w *World) { w.Intro = "\x1b]52;clipboard" },
		"empty answer":        func(w *World) { p := w.Puzzles["the_one"]; p.Answers = []string{" "}; w.Puzzles["the_one"] = p },
		"unreachable key":     func(w *World) { w.Rooms["lobby"].Exits["east"] = Exit{To: "storage", Key: "access_crystal"} },
		"bad reward":          func(w *World) { w.Rewards["root_console"] = Reward{Kind: "exec", Content: "/bin/sh"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := defaultWorld(t)
			mutate(w)
			if err := w.Validate(); err == nil {
				t.Fatal("invalid world accepted")
			}
		})
	}
}
func FuzzGameInput(f *testing.F) {
	for _, input := range []string{"look", "cat /etc/passwd", "\x1b[31m<script>", "use security_seal", "the one", ""} {
		f.Add(input)
	}
	w := defaultWorld(f)
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 2048 {
			t.Skip()
		}
		s := NewSession(w, nil)
		s.Run(input)
		if _, ok := w.Rooms[s.Room]; !ok {
			t.Fatal("invalid room")
		}
		s.Prompt()
	})
}
