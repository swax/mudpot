# MudPot

A honeypot disguised as a text-based MUD (Multi-User Dungeon). It listens on configurable TCP, TLS, and SSH ports, luring network scanners into exploring a fake space station while logging everything they do.

Visitors connect and find themselves in "Grey Sector" — the abandoned lower levels of a space station inspired by Babylon 5. They can explore rooms with text commands (`look`, `go north`, `read`, `take`, etc.), solve puzzles, unlock doors with found items, and ultimately reach a vault full of bogus secrets. The rooms are themed around mysterious station infrastructure with lore scattered throughout.

## Protocol Detection

MudPot detects and responds to non-MUD protocols automatically:

- **Telnet** — Full MUD experience with IAC sequence handling
- **SSH** — Accepts all credentials (logging usernames/passwords), then serves the MUD over an interactive shell
- **HTTP** — Serves a themed HTML page with fake navigation links and a command form
- **FTP** — Mimics a file server with fake directory listings and file contents
- **TLS** — Accepts TLS handshakes with an auto-generated self-signed certificate

Bot probes and common credential attempts (`root`, `admin`, `sudo`, `wget`, etc.) get in-character responses that waste time and encourage further interaction.

## Running

```
npm install
npm run build
npm start
```

The server listens on port `2222` (TCP) and `2223` (SSH) by default. Connect with:

```
telnet localhost 2222
ssh localhost -p 2223
```

## Configuration

Set environment variables to customize ports and logging. Multiple ports can be comma-separated. See `.env.example` for a full reference.

| Variable | Default | Description |
|---|---|---|
| `MUDPOT_PORT` | `2222` | TCP port(s) for telnet/MUD |
| `MUDPOT_SSH_PORT` | `2223` | SSH port(s) |
| `MUDPOT_TLS_PORT` | *(none)* | TLS port(s) |
| `MUDPOT_LOG` | `/var/log/mudpot.log` | Log file path (auto-rotates at 10MB) |

Example running on common service ports:

```
MUDPOT_PORT=21,23,80,2323,8080 MUDPOT_SSH_PORT=22,2222 MUDPOT_TLS_PORT=443,8443 npm start
```

## Running with PM2

To keep MudPot running in the background and restart it on crashes or reboots:

```
npm install -g pm2
pm2 start dist/server.js --name mudpot
pm2 startup
pm2 save
```

## Development

```
npm run lint        # Check code style (ESLint)
npm run lint:fix    # Auto-fix lint issues
npm run format      # Format with Prettier
npm test            # Build and run the victory walkthrough test
```

## Defenses

- Max 20 concurrent connections
- 60-second idle timeout
- Rate limit: 60 commands per minute per connection
- 4KB input buffer limit, 256-character line limit
- Telnet IAC stripping, Ctrl+C/Ctrl+D detection

## Dashboard

`src/index.php` is a stats dashboard that parses the log file and shows session history, room traffic, top commands, and how far visitors got through the maze. Serve it with any PHP-capable web server (e.g. `php -S localhost:8080`).

## Deployment

### Dedicated User

Run MudPot under its own unprivileged user account rather than root:

```
sudo useradd -r -s /usr/sbin/nologin -d /home/mudpot -m mudpot
```

If binding to privileged ports (< 1024), grant the Node.js binary the capability instead of running as root:

```
sudo setcap cap_net_bind_service=+ep $(which node)
```

### fail2ban

MudPot ships log lines that work with fail2ban to auto-ban repeat scanners.

Create the filter at `/etc/fail2ban/filter.d/mudpot.conf`:

```ini
[Definition]
failregex = \[<HOST>(:\d+)?\] CONNECTED
```

Add a jail to `/etc/fail2ban/jail.local`:

```ini
[mudpot]
enabled  = true
filter   = mudpot
logpath  = /var/log/mudpot.log
port     = 21,22,23,2222,2323,8080,8443
findtime = 5m
maxretry = 10
bantime  = 1h
```

This bans any IP that connects 10 times within 5 minutes for 1 hour. Pair with incremental ban times in the `[DEFAULT]` section for escalating bans on repeat offenders:

```ini
[DEFAULT]
bantime.increment = true
bantime.overalljails = true
bantime.maxtime = 4w
bantime.multipliers = 1 5 30 60 300 720 1440 2880
```

Reload after changes:

```
sudo fail2ban-client reload
```

## Requirements

- Node.js
- PHP (for the dashboard only)
- fail2ban (optional, for auto-banning)
