#!/usr/bin/env bash
# Destructive only to the fresh MudPot installation created by this test.
# Refuses existing installations/accounts. Use a disposable VM or CI runner.
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
[[ $EUID -eq 0 ]] || { echo 'Run as root on a disposable Linux system.' >&2; exit 1; }
[[ ${MUDPOT_SERVICE_TEST:-} == 1 ]] || { echo 'Set MUDPOT_SERVICE_TEST=1 to run the isolated installation test.' >&2; exit 1; }
[[ $(cat /proc/1/comm) == systemd ]] || { echo 'systemd must be PID 1.' >&2; exit 1; }
for tool in go cc python3 systemctl systemd-analyze runuser nsenter ssh ssh-keygen; do
    command -v "$tool" >/dev/null || { echo "Missing test dependency: $tool" >&2; exit 1; }
done
for path in /opt/mudpot /etc/mudpot /var/lib/mudpot /var/log/mudpot \
    /etc/systemd/system/mudpot.service /etc/systemd/system/mudpot-dashboard.service \
    /etc/systemd/system/mudpot.service.d /etc/systemd/system/mudpot-dashboard.service.d \
    /etc/systemd/system/multi-user.target.wants/mudpot.service \
    /etc/systemd/system/multi-user.target.wants/mudpot-dashboard.service; do
    [[ ! -e $path && ! -L $path ]] || { echo "Refusing existing path: $path" >&2; exit 1; }
done
for account in mudpot mudpot-dashboard; do
    ! getent passwd "$account" >/dev/null || { echo "Refusing existing user: $account" >&2; exit 1; }
done
for group in mudpot mudpot-dashboard mudpot-logs; do
    ! getent group "$group" >/dev/null || { echo "Refusing existing group: $group" >&2; exit 1; }
done
for unit in mudpot.service mudpot-dashboard.service; do
    [[ $(systemctl show "$unit" -p LoadState --value) == not-found ]] || { echo "Refusing existing unit: $unit" >&2; exit 1; }
done

REPORT="${MUDPOT_TEST_REPORT_DIR:-$ROOT/.tools/linux-services}"
mkdir -p "$REPORT"
REPORT="$(cd "$REPORT" && pwd)"
STAGE=$(mktemp -d /tmp/mudpot-services.XXXXXX)
STARTED="@$(date +%s)"
cleanup() {
    status=$?
    trap - EXIT
    systemctl show mudpot.service mudpot-dashboard.service > "$REPORT/service-properties.txt" 2>&1 || true
    journalctl -u mudpot.service -u mudpot-dashboard.service --since "$STARTED" --no-pager > "$REPORT/journal.txt" 2>&1 || true
    systemctl disable --now mudpot.service mudpot-dashboard.service >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/mudpot.service /etc/systemd/system/mudpot-dashboard.service
    systemctl daemon-reload
    systemctl reset-failed mudpot.service mudpot-dashboard.service >/dev/null 2>&1 || true
    # Delete fixed test-created files only; unexpected files make rmdir fail rather than being erased.
    rm -f /opt/mudpot/mudpot /opt/mudpot/write-probe
    rm -f /opt/mudpot/adventures/grey-sector/{adventure,rooms,puzzles,rewards}.json
    rm -f /etc/mudpot/{honeypot,dashboard}.env
    rm -f /var/lib/mudpot/state/{ssh_host_ed25519,tls_key.pem,tls_cert.pem}
    rm -f /var/log/mudpot/events.jsonl /var/log/mudpot/events.jsonl.1
    for dir in /opt/mudpot/adventures/grey-sector /opt/mudpot/adventures /opt/mudpot /etc/mudpot /var/lib/mudpot/state /var/lib/mudpot /var/log/mudpot; do
        if [[ -d $dir ]]; then rmdir "$dir" || status=1; fi
    done
    for account in mudpot-dashboard mudpot; do
        if getent passwd "$account" >/dev/null; then userdel "$account" || status=1; fi
    done
    for group in mudpot-dashboard mudpot mudpot-logs; do
        if getent group "$group" >/dev/null; then groupdel "$group" || status=1; fi
    done
    # STAGE is exclusively this test's mktemp directory, on the Linux filesystem.
    case "$STAGE" in /tmp/mudpot-services.*) rm -rf --one-file-system -- "$STAGE" ;; *) status=1 ;; esac
    printf 'Result: %s; reports: %s\n' "$status" "$REPORT"
    exit "$status"
}
trap cleanup EXIT

mkdir "$STAGE/source"
tar -C "$ROOT" --exclude=.git --exclude=.tools --exclude=bin --exclude=data --exclude=.env -cf - . | tar -C "$STAGE/source" -xf -
python3 "$ROOT/scripts/test-linux-services.py" configure "$STAGE"
bash "$STAGE/source/deploy.sh" 2>&1 | tee "$REPORT/install.txt"
systemd-analyze verify /etc/systemd/system/mudpot.service /etc/systemd/system/mudpot-dashboard.service 2>&1 | tee "$REPORT/unit-verification.txt"
python3 "$ROOT/scripts/test-linux-services.py" production "$STAGE" | tee "$REPORT/production.txt"

# A second install exercises binary replacement and preservation of operator configuration.
bash "$STAGE/source/deploy.sh" 2>&1 | tee "$REPORT/reinstall.txt"
python3 "$ROOT/scripts/test-linux-services.py" upgrade "$STAGE" | tee "$REPORT/upgrade.txt"

# Exercise both real unit sandboxes with instrumented executables and concurrent clients.
(cd "$STAGE/source" && CGO_ENABLED=1 go build -race -o "$STAGE/mudpot-race" ./cmd/mudpot)
systemctl stop mudpot.service mudpot-dashboard.service
install -o root -g root -m 0755 "$STAGE/mudpot-race" /opt/mudpot/mudpot
systemctl start mudpot.service mudpot-dashboard.service
python3 "$ROOT/scripts/test-linux-services.py" race "$STAGE" | tee "$REPORT/race-services.txt"
systemctl stop mudpot.service mudpot-dashboard.service
journalctl -u mudpot.service -u mudpot-dashboard.service --since "$STARTED" --no-pager > "$REPORT/journal.txt"
if grep -E 'WARNING: DATA RACE|fatal error:|ThreadSanitizer' "$REPORT/journal.txt"; then
    echo 'Instrumented services reported a race or fatal runtime error.' >&2
    exit 1
fi
systemd-analyze security --no-pager mudpot.service mudpot-dashboard.service > "$REPORT/security.txt"
printf 'Linux service installation, upgrade, isolation, lifecycle, and live race checks passed.\n'
