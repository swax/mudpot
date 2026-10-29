# Linux deployment and isolation

Deploy on a dedicated VM with no production credentials or mounts. The honeypot
and private dashboard run as different users. The dashboard can read history but
cannot edit them or read transport keys. Public listeners never serve dashboard
routes or application/configuration files.

## Install with systemd

With a patched Go toolchain and sudo access, run from the repository:

```sh
bash ./deploy.sh
```

The script builds the Go binary and installs:

| Path | Owner / access |
| --- | --- |
| `/opt/mudpot/mudpot` | root-owned executable |
| `/opt/mudpot/adventures/grey-sector` | root-owned, readable adventure files |
| `/etc/mudpot/honeypot.env` | root + honeypot user group, mode 0640 |
| `/etc/mudpot/dashboard.env` | root + dashboard user group, mode 0640 |
| `/var/lib/mudpot` | honeypot only, mode 0700 |
| `/var/log/mudpot` | honeypot writes; shared log group reads, mode 2750 |
| `/var/log/mudpot/events.jsonl` | honeypot writes; shared log group reads, 0640 |
| `/var/log/mudpot/events.jsonl.sqlite` | persistent history; honeypot writes, shared log group reads, 0640 |

It creates `mudpot`, `mudpot-dashboard`, and the `mudpot-logs` group, installs the
two unit files, then enables and restarts both services. Existing environment
files and the installed adventure directory are preserved on subsequent runs.
Review them when updating. Source configuration is not sourced as a shell script.

Default public ports are 2222 and 8081 (TCP), 2223 (SSH), and 8443 (TLS). The
dashboard binds only to `127.0.0.1:8080`. No host capabilities are granted.

```sh
sudo systemctl status mudpot mudpot-dashboard
sudo journalctl -u mudpot -u mudpot-dashboard
```

Set a dashboard password in `/etc/mudpot/dashboard.env` using
`MUDPOT_DASHBOARD_TOKEN=<long-random-value>`, then restart `mudpot-dashboard`.
Access it remotely through your host's real administrative SSH service:

```sh
ssh -L 8080:127.0.0.1:8080 operator@honeypot-host
```

Use a separate administrative port/address from the honeypot SSH listener. Do
not redirect or replace your real SSH service until an alternate access path is
working. The app never registers the decoy private key with the host SSH service.

The units use separate users, empty capability sets, no new privileges, strict
filesystem protection, private temporary storage, namespace/kernel restrictions,
and memory/process/file-descriptor caps. The dashboard additionally blocks
non-loopback network traffic at the systemd level, where supported by the host.
Review `systemd-analyze security mudpot.service mudpot-dashboard.service` on the
target OS; the exact protections available depend on its kernel and systemd.

## Publish the dashboard through Caddy

The existing read-only dashboard can also be published over HTTPS. This shares
the same visitor IPs, usernames, command text, and session history shown locally.
Visitors to the public URL do not need a password; the backend remains bound to
loopback with its token enabled.

Set the dashboard address to `127.0.0.1:18080` in `/etc/mudpot/dashboard.env` (or
adjust the example to match your configured port). Use
[`deploy/caddy-dashboard.example`](../deploy/caddy-dashboard.example) as a Caddy
snippet, replacing `REPLACE_WITH_DASHBOARD_TOKEN` with the existing dashboard
token. Store the rendered snippet at `/etc/caddy/private/mudpot-public.caddy`,
owned by `root:caddy` with mode `0640`, outside every web document root.

Import it inside the chosen HTTPS site's `route` block, after private-file rules
and before its fallback handler:

```caddyfile
route {
    import private_files
    import /etc/caddy/private/mudpot-public.caddy
    # Other handlers and the site's existing fallback follow here.
}
```

The snippet redirects `/mudpot` to `/mudpot/`, strips the path prefix, sets the
loopback Host header, and supplies the backend token. Browser-supplied credentials
are replaced. Only the site's `/mudpot/` path is published; the backend's path,
method, authentication, and resource restrictions remain in effect. Update the
private snippet whenever the backend token changes.

Validate the complete Caddy configuration before reloading it. With this snippet,
the public URL is `https://your-domain.example/mudpot/` on your configured domain.

## Restrict outbound connections

The app does not initiate outbound connections, but OS egress rules limit what
a compromised process could do. `deploy/outbound.nft.example` illustrates nftables
rules allowing established replies while rejecting new outbound traffic from
the `mudpot` UID, for both IPv4 and IPv6. Adapt and install these through your
host's firewall management. The deployment script does not modify firewall rules.

Keep firewall packet/connection-rate limits in front of the service as well.
Application limits apply after the OS accepts a connection; they cannot protect
against link saturation, all SYN floods, or every cryptographic CPU attack.

## Low service ports

Use root-managed firewall redirection rather than granting capabilities to a
shared runtime or running the application as root. For example, in an existing
nftables NAT prerouting chain, redirect an exposed test interface's TCP port 23
to 2222, port 80 to 8081, and port 443 to 8443. Scope rules to the intended
interface/address and account for IPv4/IPv6. Keep port 8080 private.

## Logs and failures

Logs rotate internally at 10 MiB with one backup. Do not run a second writer or
an external rename-based log rotation job against the same path. The honeypot
also commits session history to an embedded SQLite database before writing the
rotating logs. The database retains summaries while pruning expired detail. Passwords
are redacted unless `-capture-passwords` is explicitly passed to `serve`.

The database defaults to the log path plus `.sqlite`, so an existing installation
automatically uses `/var/log/mudpot/events.jsonl.sqlite` without changing preserved
environment files. To override it, set `MUDPOT_HISTORY` in both environment files
or pass `-history` to both commands. Keep it in a directory writable by the
honeypot and readable by `mudpot-logs`; the dashboard needs no write access.
SQLite uses rollback journaling so the read-only dashboard account does not need
to create WAL or shared-memory files. Use one honeypot writer per history/log path.

After upgrading the binary, restart the honeypot first. The first startup imports
all valid records remaining in both JSONL files. A persistent import marker prevents
old log files from resurrecting expired events on later restarts. Database writes
commit before the corresponding JSONL records, so replay is unnecessary after
that initial import. Previously deleted logs cannot be recovered. The first upgrade
from the earlier history schema scans existing events once to build progress
summaries before pruning. This can take longer on a large database; ordinary
restarts skip this scan. The dashboard may return 503 until initialization finishes.

### Retention and reports

| Record | Retention |
| --- | --- |
| Daily statistics and compact per-IP counters | Permanent |
| Summaries of sessions with successful game actions | Permanent |
| Gameplay event details | 180 days from each event |
| Routine event details and inactive routine session records | 7 days |

Successful movement, reading an actual clue, collecting an item, solving a puzzle,
or recovering a reward qualifies a session as playing. Sending `whoami`, `ls`,
`look`, unknown commands, or merely connecting does not. Promotion to gameplay
extends the retention of the session's earlier events to 180 days as well.

The default report groups players by source IP and shows each host's best session.
A session summary retains the furthest milestone, distinct rooms, clues, items,
puzzles, rewards, first/last activity, duration, username and transport. Backtracking
does not reduce the milestone. New connections record the adventure title and room
count; older imports may only have the number of visited rooms. Routes retain
the first 64 stops, with a truncation notice; milestone counts still track subsequent
progress. The per-category distinct lists are capped at 256 identifiers, matching
the world's room/puzzle/reward limits and keeping summaries compact.

Browse a host's gameplay sessions, recent connections, or traffic statistics in
separate tabs. Daily statistics remain stored permanently; the statistics tab shows
the latest 30 recorded days. Transcripts load only on the session detail page,
100 events at a time. Expired transcripts display an explicit notice while their
summaries and lifetime counts remain available.

Search is an indexed prefix lookup in player summaries, including IP, username,
session, adventure, rooms, clues, items, puzzles and rewards. It does not search
raw command/event text. At most 501 index entries are examined, with 500 eligible
matches and a notice to refine broad searches. Pagination reads 51 rows to show
50 results and an Older link. Cursor navigation avoids increasing OFFSET scans
as history grows. Lifetime cards use one counter row rather than aggregating all
sessions on each request. Command statistics use a fixed vocabulary of categories;
random scanner strings never create permanent command keys.

### Offline Country and ASN data

The optional dashboard GeoIP directory defaults to `data/geoip` under its working
directory, or `MUDPOT_GEOIP_DIR` / `-geoip-dir`. Install both
`GeoLite2-Country.mmdb` and `GeoLite2-ASN.mmdb`. The repository's `data/` directory,
download archives inside it, and all `.mmdb` files are gitignored.

`deploy.sh` installs an available pair in `/opt/mudpot/data/geoip/`, owned by root
and readable by the dashboard and GoAccess. The same files serve both applications;
no API credentials are needed for local queries. Keep license and copyright files
with your database archives. The dashboard includes MaxMind attribution.

The dashboard opens the readers once, decodes only displayed gameplay hosts, and
uses a bounded 2,048-entry LRU cache. Unknown addresses are cached too; private,
loopback and invalid addresses skip lookups. Raw scanner connections do not trigger
enrichment. The reader's own string cache is disabled, leaving one bounded IP cache.
No DNS requests, external geolocation calls, or enrichment workers run in the game
server. Missing pairs disable the feature; a partial pair, invalid type or corrupt
database is a startup error. Country is left unknown when the location database has
no country, rather than substituting the network operator's registered country.

GoAccess needs a build with GeoIP2/MMDB support. Add the two `geoip-database` paths
from `deploy/goaccess-geoip.conf.example` to its actual configuration or launcher.
A launcher using `--no-global-config` must receive the database paths as CLI flags;
editing `/etc/goaccess/goaccess.conf` will not affect it. Keep output and terminal
DNS resolution disabled for offline operation. Existing host rows can receive
country/ASN labels when restored without rereading old logs. Geographic/ASN
aggregate panels accumulate data parsed with GeoIP enabled; rebuilding historical
aggregates is a separate operation and should not reset existing request statistics.

When refreshing data, stop the dashboard and wait for any GoAccess render to finish.
Publish each new file with an atomic rename rather than overwriting an open memory
mapping, then restart the dashboard. The next GoAccess run opens the refreshed files.
An updater needs its own MaxMind download credentials; they are not needed by the
honeypot, dashboard, or report renderer and should stay outside Git.

### Resource usage

The writer batches up to 128 events over a 100 ms window, with a bounded queue.
Each batch writes one summary per affected session and updates aggregate counters.
Retention runs in indexed batches of at most 128 events and 128 routine session
records, yielding to active writes. It also amortizes deletion against incoming
batches so routine cleanup can keep pace with traffic. Expiration is gradual;
a large backlog is removed over multiple small sweeps rather than a large blocking
delete. No periodic full VACUUM runs: freed database pages are reused for new data,
so the on-disk file can retain its high-water size after pruning.

Each SQLite connection has a 2 MiB page-cache target. The dashboard permits one
uncached render at a time, with a one-second query deadline and a maximum of four
cached responses (at most 512 KiB each), retained for 5 seconds. Authentication is
checked before serving cached responses. No background report refreshes or raw
transcript scans are performed by the default dashboard.

The supplied systemd units use `Nice=10`, idle I/O scheduling, and `GOMAXPROCS=1`.
The honeypot has `CPUQuota=20%` and a 96 MiB Go soft memory limit; the dashboard
has `CPUQuota=10%` and a 48 MiB soft limit. The existing hard memory limits remain
256 MiB and 128 MiB respectively. CPU quotas are fractions of one core, including
on multicore hosts. These budgets may increase latency under load but keep CPU
usage bounded. Change them through systemd drop-ins if your host needs different
budgets. Manual CLI runs do not inherit the systemd limits. OS protections and
traffic limits remain necessary for traffic that reaches the kernel before the app.

Permanent storage grows with gameplay sessions and distinct source IPs; expiring
event detail avoids growth proportional to every historical probe. Include the
database in backups. Stop the honeypot cleanly before copying the `.sqlite` file
for a simple consistent backup, then restart it. Preserve the database when
upgrading or moving the service.

A disk write failure stops the honeypot; systemd restarts it after five seconds.
Fix disk capacity or permissions if failures persist. A full history queue applies
backpressure to connections rather than discarding their events. The queue and
connection counts remain bounded. Events still waiting in memory during an
abrupt process kill or host failure may be lost; clean shutdown drains the queue.
History persistence failures stop the honeypot just like log write failures.

On Windows, Unix permission modes do not replace NTFS ACLs. Use separate service
accounts and explicit ACLs for configuration, logs, and keys if deploying there.
The supplied service isolation is for Linux; Windows builds remain useful for
local development and testing.

## Verify the service installation

On a disposable Linux VM or CI runner with systemd as PID 1, Go, a C compiler,
Python 3, OpenSSH clients, and util-linux tools installed:

```sh
go test -race ./... -timeout 90s
sudo env "PATH=$PATH" "GOMODCACHE=$(go env GOMODCACHE)" \
  MUDPOT_SERVICE_TEST=1 bash scripts/test-linux-services.sh
```

The installation test refuses existing MudPot paths, units, users, and groups.
It deploys a private copy using random loopback-only ports, then checks the
adventure, TLS and SSH, dashboard authentication, user/capability boundaries,
read-only mounts, log/key permissions, crash recovery, and rotation during
dashboard reads. A repeat deployment must preserve environment files, adventure
settings, and transport keys. Finally both services run instrumented binaries
under the actual unit sandboxes while concurrent clients connect and systemd
delivers SIGTERM with incomplete handshakes still open.

Its exit trap removes the test-created installation, units, and accounts on
success or failure. Reports and the service journal are retained under
`.tools/linux-services/`, or `MUDPOT_TEST_REPORT_DIR` when set. Run it only on an
isolated test machine; it temporarily creates real systemd services and users.
The Linux CI job runs this check as well as the native Go race suite.
The [verification record](linux-verification.md) documents the tested platform
and completed checks.

## Migrating

Stop the v1 PM2 process and remove its boot startup entry before using its ports.
Disable the old PHP dashboard route and retire its web directory. Keep any old
text logs as an archive; v2 reads/writes JSONL and does not migrate legacy logs,
keys, or certificates. Nothing in `deploy.sh` deletes the legacy installation.
