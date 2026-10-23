# MudPot v2

A **MUD-themed honeypot for LLM-driven scanners and attacks**: a canary in the coal
mine for AI-assisted probes.

MudPot presents a text adventure to visitors probing your services. From the
first prompt, it promises system access, a private key, and credentials at the
end of the journey. Custom responses steer common scanner commands toward
exploring rooms, collecting items, and solving puzzles.

The aim is to catch scanners that can read instructions, follow clues, and adapt
their behavior. Adventure progress is a signal to investigate; it does not prove
that an LLM was involved.

All system access and rewards are simulated. Visitor commands never execute on
the host, and the adventure never exposes real credentials or host files.

## How it works

- Accept connections over TCP/Telnet and SSH, with optional TLS and responses to
  HTTP and FTP probes.
- Invite visitors into the game with hints and valid commands appropriate to
  their progress.
- Record commands, hints, room visits, and recovered decoy artifacts.
- Review activity in a separate, private dashboard.

Written in Go, with no Node.js, PHP, frontend build, or database required. The
included Grey Sector adventure spans seven rooms aboard an abandoned space
station.

## Quick start

Install a patched Go release (1.26 or later), then:

```sh
go build -trimpath -o bin/mudpot ./cmd/mudpot
./bin/mudpot serve
```

Connect with `telnet localhost 2222` or `ssh -p 2223 visitor@localhost`. SSH accepts
arbitrary passwords and public keys for the simulated recovery terminal.

Start the dashboard in a second terminal:

```sh
./bin/mudpot dashboard
```

Open <http://127.0.0.1:8080> to review activity. The dashboard binds only to
loopback and runs separately from the public honeypot. Set
`MUDPOT_DASHBOARD_TOKEN` to require authentication; see the
[deployment guide](docs/deployment.md) for remote access through an SSH tunnel.
To share it publicly, see [publishing through Caddy](docs/deployment.md#publish-the-dashboard-through-caddy).

On Windows, build with `go build -o bin/mudpot.exe ./cmd/mudpot` and use
`.\bin\mudpot.exe` for the commands above.

## Create your own adventure

```sh
./bin/mudpot init -dir my-adventure
# Edit the JSON files in my-adventure.
./bin/mudpot validate -adventure my-adventure
./bin/mudpot serve -adventure my-adventure
```

Customize the story and scanner responses in `adventure.json`, the map and items
in `rooms.json`, challenges in `puzzles.json`, and decoy rewards in `rewards.json`.
Restart the service after editing. Each visitor has independent progress.

See the [adventure authoring guide](docs/adventures.md) for examples, configuration
rules, and a walkthrough.

## Deployment and development

Use a dedicated, isolated host for public deployment. The Linux installer
provides separate service users and hardened systemd units. Keep Go and
dependencies patched, and keep adventure rewards fictional.

- [Linux deployment, isolation, and migration](docs/deployment.md)
- [Environment settings](.env.example) (`.env` is not loaded automatically)
- [Linux service and race-test verification](docs/linux-verification.md)

Run local checks with:

```sh
go test ./...
go vet ./...
go test -race ./...  # requires a supported C compiler
```
