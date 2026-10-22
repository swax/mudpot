# MudPot v2

A configurable Go honeypot disguised as a text adventure. Visitors arrive at an
administrator recovery terminal with an explicit objective: complete the journey
to obtain root system access, an SSH private key, service credentials, and backup
locations. All access and artifacts are simulated. Commands never execute on the
host, downloads never run, and artifact paths never open host files.

The included Grey Sector adventure takes visitors through seven rooms aboard an
abandoned space station. Create your own setting, routes, key items, challenges,
and reward files by editing JSON. No Node.js, PHP, frontend build, or database is
required. The only direct Go module dependency is `golang.org/x/crypto`, for SSH.

## Quick start

Install a current, patched Go release (Go 1.26 or later), then:

```sh
go build -trimpath -o bin/mudpot ./cmd/mudpot
./bin/mudpot serve
```

On Windows the executable is `bin/mudpot.exe`; build with
`go build -o bin/mudpot.exe ./cmd/mudpot` and use that path below.

Connect using `telnet localhost 2222` or `ssh -p 2223 visitor@localhost`.
SSH accepts arbitrary passwords and public keys for the fake recovery terminal.
Its host key is generated on first startup. A connection stays open after victory
so visitors can explore the simulated console and read the recovered artifacts.

Start the private dashboard in a **second process**:

```sh
./bin/mudpot dashboard
```

Open <http://127.0.0.1:8080>. It displays session history, room traffic, commands,
and vault recoveries from the JSONL event log. It binds only to a loopback IP,
has no write endpoints, and never starts as part of the public honeypot.

## Create an adventure

```sh
./bin/mudpot init -dir my-adventure
# Edit the four JSON files in my-adventure.
./bin/mudpot validate -adventure my-adventure
./bin/mudpot serve -adventure my-adventure
```

| File | What to customize |
| --- | --- |
| `adventure.json` | Title, simulated hostname, starting room, introduction, victory text, bot responses |
| `rooms.json` | Room descriptions, exits, collectible items, gates, reward placement |
| `puzzles.json` | Answer challenges and their prompts, accepted answers, and messages |
| `rewards.json` | Simulated consoles, credential files, aliases, and file contents |

The starter is embedded in the binary. `init` copies it into a new directory and
refuses to overwrite one. To edit the checked-in example directly, run with
`-adventure adventures/default`. Configuration changes take effect after restart;
changing the embedded starter requires rebuilding. Every connection gets its own
inventory, collected items, unlocked gates, and puzzle state.

See [the adventure authoring guide](docs/adventures.md) for complete examples,
validation rules, and a walkthrough.

## Listeners and settings

```sh
./bin/mudpot serve -tcp :2222,:8081 -ssh :2223 -tls :8443
./bin/mudpot serve -tcp 127.0.0.1:2222 -ssh ''
./bin/mudpot serve -h
```

TCP and TLS listeners recognize MUD/Telnet, HTTP, and FTP probes. HTTP serves a
recovery invitation; the playable adventure is available over Telnet and SSH.
FTP is a bounded control-channel simulation: no data connections or file transfers
are opened. SSH on a plain TCP port receives a banner only; use an SSH listener
for an interactive SSH session. TLS is 1.2 or later with a generated self-signed
certificate; generated transport keys stay in the private state directory.

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `MUDPOT_PORT` | `:2222` | TCP listen addresses or bare ports, comma separated |
| `MUDPOT_SSH_PORT` | `:2223` | SSH listen addresses or ports; empty disables |
| `MUDPOT_TLS_PORT` | empty | TLS listen addresses or ports |
| `MUDPOT_ADVENTURE` | empty | Adventure directory; empty selects embedded starter |
| `MUDPOT_STATE` | `data/state` | Private host-key and certificate directory |
| `MUDPOT_LOG` | `data/events.jsonl` | Honeypot output / dashboard input |
| `MUDPOT_DASHBOARD_ADDR` | `127.0.0.1:8080` | Private dashboard bind address |
| `MUDPOT_DASHBOARD_TOKEN` | empty | Optional dashboard password / bearer token |

Flags override environment settings. The binary does **not** automatically load
`.env`. `.env.example` documents service settings; supply environment variables
through your process manager or shell. Configuration and environment files are
never served by either listener.

## Private dashboard access

For remote use, keep the dashboard on loopback and tunnel to it:

```sh
ssh -L 8080:127.0.0.1:8080 operator@honeypot-host
```

Then open <http://127.0.0.1:8080> locally. Set `MUDPOT_DASHBOARD_TOKEN` to a long,
random value to require authentication. The browser prompts for a username and
password; use any username and the token as the password. Monitoring clients may
instead send `Authorization: Bearer <token>`. Tokens in URLs are not accepted.

The dashboard reads at most the last 2 MiB of each of the current and rotated
logs. Statistics describe retained observations, not lifetime totals. A sample
notice appears when the read or session cap is reached. Up to 50 recent sessions
are shown, with up to 40 recent events each. Reload the page to refresh.

## Security boundaries

- 20 simultaneous connections across all public listeners, including incomplete
  SSH/TLS handshakes; 4 connections per IP by default.
- 5-second protocol/crypto handshake deadlines, 60-second read-idle deadlines,
  5-second socket write deadlines, and a 10-minute absolute connection lifetime.
- 256-byte command lines, 60 commands/minute and 600 commands per connection,
  including FTP commands and empty lines. No unbounded application command queue.
- 256 KiB incoming and 1 MiB outgoing wire budgets per connection, plus 8 KiB
  HTTP-header parsing and one SSH session channel per connection.
- SSH forwarding and non-session channels are rejected. Exec requests enter the
  adventure engine; they do not launch a shell, interpreter, or subprocess.
- JSONL records escape embedded newlines and controls. Logging uses a 1,024-event
  queue, 1,024-byte text fields, 10 MiB rotation, and one backup. Disk failures stop
  the public service; queue overload drops events and reports the count on
  shutdown instead of allowing unbounded memory growth.
- Password capture is disabled by default, including FTP passwords. Opt in with
  `-capture-passwords` when you intend to retain these in the private logs.
- Dashboard templates escape visitor text; CSP, host checks, bounded reads,
  connection caps, and request deadlines provide additional protection.
- No real root terminal, command execution, outbound HTTP client, arbitrary
  filesystem endpoint, or public dashboard API is implemented.

The decoy key is a fresh, parseable Ed25519 private key generated **in memory per
visitor session**, unrelated to transport keys and never authorized on any host.
The example service endpoints use reserved `.invalid` domains. Keep your own
adventure rewards fictional; do not paste operational secrets into configuration.

See [deployment and isolation](docs/deployment.md) for separate Linux users,
systemd hardening, outbound firewall rules, and privileged-port forwarding.
Language and application limits do not replace OS isolation or security updates.

## Develop and verify

```sh
go test ./...
go vet ./...
go test -race ./...                 # needs a supported C compiler
go test ./internal/adventure -fuzz FuzzGameInput -fuzztime 10s
go test ./internal/honeypot -fuzz FuzzTerminal -fuzztime 10s
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Tests cover the journey, configurable rewards, per-session state, malformed
configuration, log injection/rotation, dashboard authentication/escaping, TCP,
TLS, SSH, channel restrictions, pre-auth caps, and protocol resource limits.
CI also runs Linux race checks, real systemd installation/isolation/lifecycle
tests, and dependency vulnerability scanning. Keep Go and modules patched and
rebuild the binary when their security updates are released.

## Migrating from v1

This replaces the Node/TypeScript server, PHP dashboard, and PM2 deployment.
Stop the old PM2 process before starting v2 on the same ports and remove its
boot-time startup entry. Retire the PHP web directory and any externally exposed
dashboard route. The new deployment never copies `.env` into a web directory.

The protocol port environment names remain supported. Log format has changed to
JSONL, with a new default path; archive old text logs separately. Old host keys
and certificates are not imported. Review v1 fail2ban filters before reuse: they
must now match JSON events, rather than the old `CONNECTED` text format.
