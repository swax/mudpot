#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/mudpot ./cmd/mudpot
bin/mudpot validate -adventure adventures/default

SUDO=()
if [[ $EUID -ne 0 ]]; then SUDO=(sudo); fi

if ! getent group mudpot-logs >/dev/null; then
    "${SUDO[@]}" groupadd --system mudpot-logs
fi
for account in mudpot mudpot-dashboard; do
    if ! id "$account" >/dev/null 2>&1; then
        "${SUDO[@]}" useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin "$account"
    fi
    "${SUDO[@]}" usermod -a -G mudpot-logs "$account"
done

"${SUDO[@]}" install -d -o root -g root -m 0755 /opt/mudpot /opt/mudpot/adventures /etc/mudpot
"${SUDO[@]}" install -d -o mudpot -g mudpot -m 0700 /var/lib/mudpot
"${SUDO[@]}" install -d -o mudpot -g mudpot-logs -m 2750 /var/log/mudpot
# Build and validate before replacing the installed executable.
"${SUDO[@]}" install -o root -g root -m 0755 bin/mudpot /opt/mudpot/mudpot

if [[ ! -e /opt/mudpot/adventures/grey-sector ]]; then
    "${SUDO[@]}" install -d -o root -g root -m 0755 /opt/mudpot/adventures/grey-sector
    "${SUDO[@]}" install -o root -g root -m 0644 adventures/default/*.json /opt/mudpot/adventures/grey-sector/
fi
if [[ ! -e /etc/mudpot/honeypot.env ]]; then
    "${SUDO[@]}" install -o root -g mudpot -m 0640 deploy/honeypot.env /etc/mudpot/honeypot.env
fi
if [[ ! -e /etc/mudpot/dashboard.env ]]; then
    "${SUDO[@]}" install -o root -g mudpot-dashboard -m 0640 deploy/dashboard.env /etc/mudpot/dashboard.env
fi

"${SUDO[@]}" install -o root -g root -m 0644 deploy/mudpot.service deploy/mudpot-dashboard.service /etc/systemd/system/
"${SUDO[@]}" systemctl daemon-reload
"${SUDO[@]}" systemctl enable mudpot.service mudpot-dashboard.service
"${SUDO[@]}" systemctl restart mudpot.service mudpot-dashboard.service
echo 'Installed MudPot v2. The private dashboard address is in /etc/mudpot/dashboard.env; see docs/deployment.md for isolation and remote access.'
