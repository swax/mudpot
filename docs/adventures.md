# Building your own adventure

Run `mudpot init -dir my-adventure`, edit the JSON files, then run
`mudpot validate -adventure my-adventure`. No code or build step is needed to use
an external adventure. These are data files, not executable templates or scripts.

## Adventure manifest

```json
{
  "schema_version": 1,
  "title": "The Lost Datacenter",
  "hostname": "recovery-server",
  "start": "reception",
  "intro": "Reach the server vault to recover administrator access, an SSH private key, and service credentials. Type help to begin.",
  "victory": "Vault recovered. Use root_console or read id_ed25519.",
  "responses": {
    "sudo": "Root access requires the recovery console in the vault.",
    "curl": "External links are offline. Recover the backup manifest in the vault."
  }
}
```

`responses` maps otherwise unhandled command names to canned replies. Built-in
commands such as `look`, `go`, `read`, and `whoami` take precedence. Keep the
reward and next useful action visible so both humans and bots know why to continue.
The optional `hostname` controls the simulated root prompt and host identity;
it defaults to `recovery`. No real host setting is changed.

## Rooms, items, and gates

`rooms.json` is an object keyed by room IDs:

```json
{
  "reception": {
    "name": "Reception",
    "description": "A badge lies on the desk. A terminal protects the northern vault.",
    "items": { "badge": "A maintenance badge marked VAULT." },
    "pickup": ["badge"],
    "exits": {
      "north": {
        "to": "vault",
        "key": "badge",
        "puzzle": "recovery_code",
        "message": "The badge wakes the challenge terminal."
      }
    }
  },
  "vault": {
    "name": "Server Vault",
    "description": "Administrator recovery is ready. Use root_console or read id_ed25519.",
    "items": {},
    "exits": { "south": { "to": "reception" } },
    "rewards": ["root_console", "id_ed25519"]
  }
}
```

Exits require `to`. A `key`, `puzzle`, and unlock `message` are optional. With
neither key nor puzzle, the exit is open. A keyed exit needs an inventory item;
with a puzzle it then asks the challenge. Successful answers unlock that exit,
and the visitor uses `go north` to pass through. `use badge` activates a matching
gate without moving. Keys are reusable and never consumed. If several exits use
the same key, `use` activates the first still-locked exit in alphabetical order;
`go <direction>` selects the desired exit explicitly.

`items` maps names to readable text. Only items listed in `pickup` can be taken.
Different visitors do not share items or door state. Item lookup accepts spaces
in place of underscores; reward IDs, paths, and aliases match exactly.

Directions can be custom IDs (`go airlock`). The six compass/vertical directions
also work as standalone commands, with `n`, `s`, `e`, `w`, `u`, `d` shortcuts.
`go n` and `move north` work too.

## Puzzles

`puzzles.json` maps challenge IDs to answer challenges:

```json
{
  "recovery_code": {
    "prompt": "What follows alpha and beta? > ",
    "answers": ["gamma", "the gamma"],
    "success": "The vault opens. Go north.",
    "failure": "The terminal rejects the answer. Try again, or cancel."
  }
}
```

Answers are case-insensitive and ignore whitespace. Wrong answers can be retried;
`cancel` or `back` leaves the panel, and `quit` disconnects. Reference a challenge
from any exit's `puzzle` field. It is solved separately for each exit and visitor.
Put discoverable clues in room items. Arbitrary regexes and executable puzzle
hooks are intentionally not part of this format.

## Simulated rewards

`rewards.json` maps reward IDs to content. Place their IDs in a room's `rewards`
array. They are accessible only in that room, even with the simulated root prompt.

```json
{
  "root_console": {
    "kind": "shell",
    "aliases": ["console"],
    "content": "Welcome, root. Use whoami, id, ls, or cat /root/.ssh/id_ed25519."
  },
  "id_ed25519": {
    "kind": "file",
    "aliases": ["/root/.ssh/id_ed25519"],
    "content": "{{decoy_ssh_private_key}}"
  }
}
```

`file` outputs text. `shell` also switches to a simulated root prompt, where
`whoami`, `id`, and `pwd` return canned root-like values. `ls` lists the current
room and its archive. Both kinds work with `read`, `cat`, or `use`. The first
reward access records one victory and prints the manifest's victory message;
the connection remains open for other rewards.

The only substitution is `{{decoy_ssh_private_key}}`: a fresh Ed25519 OpenSSH-format
private key generated in memory for the session. Repeated reads return the same
key; other sessions get different keys. It is never installed in authorized_keys,
written to disk, or derived from real SSH/TLS transport keys. Other content is
literal text. Aliases such as `/root/.ssh/...` are labels, not filesystem paths.
Pipelines, substitutions, redirections, and scripts are never interpreted.

## Validation rules

- Each JSON file is at most 512 KiB. Unknown fields and trailing JSON are rejected.
- Use schema version 1, 1–256 rooms, 1–256 rewards, and up to 256 puzzles.
- Room, item, exit, puzzle, reward, and response IDs match
  `[a-z][a-z0-9_.-]{0,63}`. Use lowercase names without spaces.
- Every exit target, gate key, puzzle, and room reward must exist. Pickup items
  need a local description. Every reward must be placed in a room.
- All rooms must be reachable with keys obtainable from accessible rooms.
  Validation assumes puzzles with configured answers are solvable; authors must
  still check their clues and intended route manually.
- Prompts, success/failure text, and at least one nonempty answer are required.
  Answers are limited to 256 bytes, matching the network command-line limit.
- Reward kinds are `file` and `shell`. Aliases are lowercase, contain no spaces
  or controls, and are unique across rewards and reward IDs.
- Individual text fields are at most 32 KiB. Terminal controls, including ANSI
  escapes, are rejected; newlines and tabs are allowed.

Restart to load edits. Invalid adventures cause startup to fail before listeners
open. The four filenames are fixed, without includes or filesystem templates.

## Grey Sector walkthrough

```text
east
take access_crystal
west
north
west
take security_seal
use access_crystal
north
east
use security_seal
the one
north
use root_console
whoami
cat /root/.ssh/id_ed25519
read credentials.env
read backup_manifest
quit
```
