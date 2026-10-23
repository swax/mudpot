package adventure

import "strings"

// guide turns a scanner reply or failed command into an actionable invitation.
// It uses the configured world and never prints puzzle answers or inaccessible loot.
func (s *Session) guide(text string) Result {
	if s.puzzleDir != "" {
		return Result{Text: strings.TrimRight(text, "\n") + "\nA challenge is active. Enter your answer, or type 'cancel' to return and look for clues.\n"}
	}
	next := s.nextCommand()
	s.emit("hint", s.Room, next)
	return Result{Text: strings.TrimRight(text, "\n") + "\nNext command: " + next + "\n"}
}

// nextCommand searches only rooms reachable with the visitor's current keys and
// solved gates. A ready puzzle is a destination, never an edge to pass through.
func (s *Session) nextCommand() string {
	type step struct{ room, firstDirection string }
	queue := []step{{room: s.Room}}
	seen := map[string]bool{s.Room: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		room := s.World.Rooms[current.room]
		action := ""
		for _, id := range room.Rewards {
			if !s.readRewards[id] {
				action = "read " + id
				if s.World.Rewards[id].Kind == "shell" {
					action = "use " + id
				}
				break
			}
		}
		if action == "" {
			for _, item := range room.Pickup {
				if !s.Inventory[item] {
					action = "take " + item
					break
				}
			}
		}
		for _, dir := range Keys(room.Exits) {
			exit := room.Exits[dir]
			unlocked := s.unlocked[current.room+"/"+dir]
			if !unlocked && exit.Key != "" && !s.Inventory[exit.Key] {
				continue
			}
			if !unlocked && exit.Puzzle != "" {
				if action == "" {
					action = "go " + dir
				}
				continue
			}
			if !seen[exit.To] {
				first := current.firstDirection
				if first == "" {
					first = dir
				}
				seen[exit.To] = true
				queue = append(queue, step{exit.To, first})
			}
		}
		if action != "" {
			if current.firstDirection != "" {
				return "go " + current.firstDirection
			}
			return action
		}
	}
	return "look"
}
