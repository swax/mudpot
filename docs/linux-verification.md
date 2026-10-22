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
