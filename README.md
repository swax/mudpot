# MudPot v2

A **MUD-themed honeypot for LLM-driven scanners and attacks**: a canary in the coal
mine for AI-assisted probes.

Visitors probing your services find a text adventure promising system access,
keys, and credentials. Scanner commands lead toward rooms, clues, and puzzles.

The aim is to catch scanners that follow clues and adapt. Progress is a signal
to investigate; it does not prove that an LLM was involved.

All system access and rewards are simulated. Visitor commands never execute on
the host, and the adventure never exposes real credentials or host files.

## How it works

- Accept TCP/Telnet, SSH, and optional TLS connections, plus HTTP and FTP probes.
- Guide visitors through a customizable adventure with simulated rewards.
- See who played and how far they got, with searchable history and lasting stats.
- Optionally show offline country and network-provider labels using MaxMind data.

Written in Go with embedded SQLite; no frontend build or database server needed.
The included Grey Sector adventure spans seven rooms on an abandoned station.

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

Open <http://127.0.0.1:8080> for **Players and progress**. The separate dashboard
binds only to loopback; set `MUDPOT_DASHBOARD_TOKEN` to require authentication.

On Windows, build with `go build -o bin/mudpot.exe ./cmd/mudpot` and use
`.\bin\mudpot.exe` for the commands above.

## Create your own adventure

```sh
./bin/mudpot init -dir my-adventure
# Edit the JSON files in my-adventure.
./bin/mudpot validate -adventure my-adventure
./bin/mudpot serve -adventure my-adventure
```

Edit the generated JSON files to customize the story, map, puzzles, and rewards.
See the [adventure authoring guide](docs/adventures.md) for examples and a walkthrough.

## Deployment and development

Use a dedicated, isolated host for public deployment. The Linux installer provides
separate service users, hardened systemd units, and low CPU limits.

- [Deployment, retention, backups, GeoIP/GoAccess, and resource settings](docs/deployment.md)
- [Publish the dashboard through Caddy](docs/deployment.md#publish-the-dashboard-through-caddy)
- [Environment settings](.env.example) (`.env` is not loaded automatically)
- [Linux service and race-test verification](docs/linux-verification.md)

Run local checks with:

```sh
go test ./...
go vet ./...
go test -race ./...  # requires a supported C compiler
```
