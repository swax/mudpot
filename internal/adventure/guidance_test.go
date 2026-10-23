package adventure

import (
	"strings"
	"testing"
)

func TestScannerResponsesSuggestPlayableCommands(t *testing.T) {
	w := defaultWorld(t)
	commands := []string{"whoami", "id", "pwd", "ls -la", "uname -a", "hostname", "cat /etc/passwd", "use nothing", "take nothing", "go missing", "unknown --option", "help"}
	commands = append(commands, Keys(w.Responses)...)
	for _, cmd := range commands {
		t.Run(cmd, func(t *testing.T) {
			s := NewSession(w, nil)
			result := s.Run(cmd)
			if !strings.Contains(result.Text, "Next command: go east\n") {
				t.Fatalf("no playable next step for %q: %s", cmd, result.Text)
			}
			if s.Room != w.Start || len(s.Inventory) != 0 || s.puzzleDir != "" || s.completed {
				t.Fatal("probe changed game progress")
			}
		})
	}
}

func TestHintsRecordOriginalProbeAndSuggestedCommand(t *testing.T) {
	var events []struct{ kind, room, text string }
	s := NewSession(defaultWorld(t), func(kind, room, text string) {
		events = append(events, struct{ kind, room, text string }{kind, room, text})
	})
	s.Run("UNAME -a")
	if len(events) != 2 || events[0].kind != "command" || events[0].text != "UNAME -a" || events[1].kind != "hint" || events[1].text != "go east" || events[1].room != "lobby" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestGuidanceRespectsCustomExitsNamedLikeDirectionAliases(t *testing.T) {
	w := defaultWorld(t)
	s := NewSession(w, nil)
	s.Run("e")
	s.Run("go w")
	if s.Room != w.Start {
		t.Fatal("standard direction aliases changed")
	}
	w.Rooms["lobby"].Exits["n"] = w.Rooms["lobby"].Exits["east"]
	delete(w.Rooms["lobby"].Exits, "east")
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := s.nextCommand(); got != "go n" {
		t.Fatal(got)
	}
	s.Run("go n")
	if s.Room != "storage" {
		t.Fatal("suggestion did not follow literal configured exit", s.Room)
	}
	s.Run("west")
	s.Run("n")
	if s.Room != "storage" {
		t.Fatal("standalone shortcut ignored literal exit", s.Room)
	}
}

func TestGuidanceFollowsJourneyAndRemainingRewards(t *testing.T) {
	s := NewSession(defaultWorld(t), nil)
	expected := []string{"go east", "take access_crystal", "go west", "go north", "go west", "take security_seal", "go north", "go east", "go north"}
	for _, want := range expected {
		if out := s.Run("sudo -n id").Text; !strings.Contains(out, "Next command: "+want+"\n") {
			t.Fatalf("room %s: want %q, got %s", s.Room, want, out)
		}
		s.Run(want)
	}
	if s.puzzleDir == "" {
		t.Fatal("did not reach challenge")
	}
	for _, cmd := range []string{"uname -a", "help", "read carved_figure", "wrong answer"} {
		out := s.Run(cmd).Text
		if !strings.Contains(out, "type 'cancel'") || strings.Contains(out, "Next command:") || strings.Contains(out, "the one") {
			t.Fatalf("invalid or spoiler puzzle guidance: %s", out)
		}
		if s.puzzleDir == "" || s.Room == "vault" {
			t.Fatal("guidance bypassed challenge")
		}
	}
	s.Run("the one")
	for _, want := range []string{"go north", "use root_console", "read id_ed25519", "read credentials.env", "read backup_manifest", "look"} {
		out := s.Run("not-a-command").Text
		if !strings.Contains(out, "Next command: "+want+"\n") {
			t.Fatalf("want %q, got %s", want, out)
		}
		s.Run(want)
	}
	if !s.completed || !s.elevated || len(s.readRewards) != 4 {
		t.Fatal("recovery did not complete")
	}
	if got := s.Run("whoami").Text; got != "root\n" {
		t.Fatalf("simulated identity changed: %q", got)
	}
}

func TestGuidanceUsesCustomWorldAndOnlyAccessibleGates(t *testing.T) {
	w := &World{
		Manifest: Manifest{SchemaVersion: 1, Title: "Custom", Start: "entry", Intro: "Find the archive.", Victory: "Recovered."},
		Rooms: map[string]Room{
			"entry":   {Name: "Entry", Description: "Choose a route.", Exits: map[string]Exit{"airlock": {To: "archive", Key: "badge"}, "supply": {To: "store"}}},
			"store":   {Name: "Store", Description: "Take a badge.", Items: map[string]string{"badge": "Access badge."}, Pickup: []string{"badge"}, Exits: map[string]Exit{"return": {To: "entry"}}},
			"archive": {Name: "Archive", Description: "Recovery ready.", Rewards: []string{"token"}, Exits: map[string]Exit{"back": {To: "entry"}, "onward": {To: "second"}}},
			"second":  {Name: "Second archive", Description: "More recovery.", Rewards: []string{"backup"}},
		},
		Rewards: map[string]Reward{"token": {Kind: "file", Content: "decoy token"}, "backup": {Kind: "file", Content: "decoy backup"}},
	}
	if err := w.Validate(); err != nil {
		t.Fatal(err)
	}
	s, other := NewSession(w, nil), NewSession(w, nil)
	for _, want := range []string{"go supply", "take badge", "go return", "go airlock", "read token", "go onward", "read backup", "look"} {
		if got := s.nextCommand(); got != want {
			t.Fatalf("room %s: want %q, got %q", s.Room, want, got)
		}
		s.Run(want)
	}
	if other.nextCommand() != "go supply" || len(other.Inventory) != 0 || len(other.readRewards) != 0 {
		t.Fatal("guidance state leaked between visitors")
	}
}

func TestCustomResponseTokensRemainLiteralAndValidated(t *testing.T) {
	for _, token := range []string{"!!huawei", "/bin/busybox", "root:root"} {
		w := defaultWorld(t)
		w.Responses[token] = "Recovery is available."
		if err := w.Validate(); err != nil {
			t.Fatalf("rejected literal token %q: %v", token, err)
		}
		out := NewSession(w, nil).Run(token).Text
		if !strings.Contains(out, "Recovery is available.\nNext command: go east") {
			t.Fatal(out)
		}
	}
	for _, token := range []string{"", "two words", "UPPER", "line\nbreak", "control\x1b", strings.Repeat("x", 257)} {
		w := defaultWorld(t)
		w.Responses[token] = "Response"
		if err := w.Validate(); err == nil {
			t.Fatalf("accepted invalid response key %q", token)
		}
	}
}
