# Linux deployment and isolation

Deploy on a dedicated VM with no production credentials or mounts. The honeypot
and private dashboard run as different users. The dashboard can read logs but
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
an external rename-based log rotation job against the same path. The dashboard
handles rotation and samples retained data with bounded memory. Passwords are
redacted unless `-capture-passwords` is explicitly passed to `serve`.

A disk write failure stops the honeypot; systemd restarts it after five seconds.
Fix disk capacity or permissions if failures persist. Under a full log queue,
events may be dropped to preserve the service's resource bounds; the count is
reported when the service stops. This is an observation tool, not a lossless
audit system.

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
