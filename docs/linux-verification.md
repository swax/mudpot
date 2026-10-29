# Linux service and race verification

Verified on 2026-10-22 with Ubuntu 22.04.5 LTS under WSL2, Linux
5.15.146.1-microsoft-standard-WSL2, systemd 249.11, GCC 11.4, and Go 1.27.3
for linux/amd64.

| Check | Result |
| --- | --- |
| Native `go test -race ./... -timeout 90s` | Passed across the module |
| Real installation and systemd unit verification | Passed |
| Adventure walkthrough and generated decoy key | Passed through the installed service |
| TLS adventure and SSH exec simulation | Passed; SSH exec could not read host files |
| Separate unprivileged accounts, empty effective capabilities, no new privileges | Passed |
| Read-only application mounts in both service namespaces | Passed |
| Private key/state modes and shared read-only log access | Passed |
| Dashboard authentication, unexpected Host rejection, read-only routes | Passed |
| Public/private listener separation | Passed |
| Forced honeypot crash and automatic restart | Passed; dashboard stayed available |
| Log rotation during concurrent dashboard reads | Passed; log permissions preserved |
| Repeat deployment | Preserved environment settings, customized adventure, and transport keys |
| Loading the operator's customized adventure after upgrade | Passed |
| Both actual service sandboxes with race-instrumented binaries and concurrent clients | Passed |
| SIGTERM with incomplete handshakes still open | Both services drained and exited with status 0 |
| Service journal audit | No data race or fatal runtime reports |
| Test cleanup | Test-created units, accounts, configuration, logs, and keys removed |

The repeatable test is `scripts/test-linux-services.sh`, backed by the Python
standard-library checks in `scripts/test-linux-services.py`. It refuses existing
installations and runs a copied configuration on random loopback ports. The
Linux CI job now runs this suite after the native race detector.

Test compatibility fixes use a portable systemd unit kill command and Unix
timestamps for journal queries, including systemd 249. The installer now points
to the configured dashboard address instead of printing a fixed port.

The run's detailed reports are retained locally in `.tools/linux/report/`:
`production.txt`, `upgrade.txt`, `race-services.txt`, `unit-verification.txt`,
`service-properties.txt`, `security.txt`, and `journal.txt`. Default runs of the
checked-in test use `.tools/linux-services/`. See [deployment](deployment.md)
for prerequisites and reproduction commands.

## Persistent session history

Verified on 2026-10-28 using the local Ubuntu/systemd test environment and Go
1.27.3. The full Go race suite passed, including history retained after multiple
rotations and deletion of both JSONL files, restart persistence, imports beyond
the former 2 MiB sample, import deduplication, browsing more than 2,000 sessions,
literal search and activity filters, and complete event pagination.

The real service installation test also passed with the SQLite history store:
the file has mode 0640, the separate dashboard account can read it but cannot
write it, and the original completed adventure remains searchable after a
forced crash, live log rotation, and repeat deployment. Race-instrumented
services handled concurrent clients and drained cleanly on SIGTERM. Test-created
services, files, and accounts were removed successfully. Reports are retained
locally in `.tools/history-linux-services/`.

Windows `go test ./...`, `go vet ./...`, and the browser checks for search,
activity filters, session navigation, and complete session details passed too.
`govulncheck ./...` reported no vulnerabilities affecting called code.

## Retention and player reports

Verified on 2026-10-28 after adding the 7-day routine / 180-day gameplay detail
policy. Windows tests and the Linux race suite passed. Coverage includes genuine
gameplay classification, host grouping, furthest milestones despite backtracking,
permanent counters and summaries after expiry, promotion of early session events,
bounded cleanup, retained-event pagination, imports that cannot resurrect pruned
detail, legacy migrations across UTC dates, and indexed cursor/prefix queries.

The isolated systemd service suite passed with the new low-priority CPU budgets
(20% of one core for the honeypot, 10% for the dashboard). It verified quota and
Nice settings, database permissions, default Players and progress reports,
progress search, crash/restart, log rotation, repeat deployment, concurrent
instrumented services, and clean shutdown. Reports are in
`.tools/retention-linux-services/`.

`BenchmarkReports` compared 500 gameplay hosts with zero versus 100,000 routine
session rows on the local Windows Intel Core i9-14900HX. Player queries took
about 1.24 ms versus 1.17 ms; statistics queries took 0.92 ms versus 0.81 ms.
Player queries allocated about 93 KiB per operation in both cases. These are
local query measurements, not latency guarantees for another host; they confirm
the normal report avoids work proportional to routine connection history.

Browser checks verified the default grouped report, milestone counts, prefix
search and recovery filtering, and an expired 200-day-old transcript whose
progress summary remains visible. Screenshots use synthetic data and are retained
locally in `.tools/retention-preview/`.

## Offline Country and ASN enrichment

Verified on 2026-10-28 with the downloaded Country and ASN MMDB files kept in
gitignored `data/geoip/`. Windows tests and Linux race tests passed, including
IPv4/IPv6 decoding, bounded positive/negative caching, concurrent access, private
address skipping, missing/invalid file handling, and player-only report enrichment.
The local cached lookup benchmark measured about 50 ns with zero allocations.
`govulncheck` found no vulnerabilities affecting called code.

The existing GoAccess 1.12 renderer was tested with synthetic logs on the target
host before activation. Restored host rows acquired country/ASN labels without
resubmitting unchanged logs or changing request totals. Live activation then
verified both services, the shared database file checksums, the updated renderer,
preserved request totals, CPU limits, and the public MudPot report with GeoIP
enabled. The database files are shared read-only at `/opt/mudpot/data/geoip/`.
No DNS or external geolocation requests were enabled. Preparation, activation
records, backup location, and the live screenshot are retained locally under
`.tools/geoip-setup/`.
