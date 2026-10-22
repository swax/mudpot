#!/usr/bin/env python3
"""Functional checks for an isolated installation made by test-linux-services.sh."""
import concurrent.futures
import hashlib
import http.client
import json
import os
from pathlib import Path
import pwd
import socket
import ssl
import stat
import subprocess
import sys
import time


def check(condition, message):
    if not condition:
        raise AssertionError(message)
    print("PASS:", message, flush=True)


def command(*args, ok=True, **kwargs):
    result = subprocess.run(args, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=20, **kwargs)
    if ok and result.returncode:
        raise AssertionError(f"{args!r}: {result.stderr or result.stdout}")
    return result


def prop(unit, name):
    return command("systemctl", "show", unit, "-p", name, "--value").stdout.strip()


def wait_for(test, message, seconds=15):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        try:
            if test():
                return
        except (OSError, http.client.HTTPException):
            pass
        time.sleep(0.1)
    raise AssertionError(message)


def configure(stage):
    held = [socket.socket() for _ in range(4)]
    for sock in held:
        sock.bind(("127.0.0.1", 0))
    ports = dict(zip(("tcp", "ssh", "tls", "dashboard"),
                     (sock.getsockname()[1] for sock in held)))
    (stage / "ports.json").write_text(json.dumps(ports))
    env_dir = stage / "source/deploy"
    (env_dir / "honeypot.env").write_text(
        f"MUDPOT_PORT=127.0.0.1:{ports['tcp']}\n"
        f"MUDPOT_SSH_PORT=127.0.0.1:{ports['ssh']}\n"
        f"MUDPOT_TLS_PORT=127.0.0.1:{ports['tls']}\n"
        "MUDPOT_ADVENTURE=/opt/mudpot/adventures/grey-sector\n"
        "MUDPOT_STATE=/var/lib/mudpot/state\n"
        "MUDPOT_LOG=/var/log/mudpot/events.jsonl\n")
    (env_dir / "dashboard.env").write_text(
        f"MUDPOT_DASHBOARD_ADDR=127.0.0.1:{ports['dashboard']}\n"
        "MUDPOT_LOG=/var/log/mudpot/events.jsonl\n"
        "MUDPOT_DASHBOARD_TOKEN=mudpot-service-test-token\n")
    for sock in held:
        sock.close()


def request(ports, path="/", token="mudpot-service-test-token", method="GET", host=None):
    conn = http.client.HTTPConnection("127.0.0.1", ports["dashboard"], timeout=5)
    headers = {"Host": host or f"127.0.0.1:{ports['dashboard']}"}
    if token:
        headers["Authorization"] = "Bearer " + token
    conn.request(method, path, headers=headers)
    response = conn.getresponse()
    result = response.status, response.read().decode()
    conn.close()
    return result


def ready(ports):
    for unit in ("mudpot.service", "mudpot-dashboard.service"):
        wait_for(lambda: prop(unit, "ActiveState") == "active", f"{unit} failed to start")
    wait_for(lambda: request(ports)[0] == 200, "Dashboard did not become ready")
    for kind in ("tcp", "ssh", "tls"):
        wait_for(lambda: can_connect(ports[kind]), f"{kind} did not become ready")


def can_connect(port):
    with socket.create_connection(("127.0.0.1", port), timeout=1):
        return True


def until(sock, suffix):
    data = bytearray()
    while not data.endswith(suffix):
        chunk = sock.recv(1)
        if not chunk:
            raise AssertionError(f"Disconnected waiting for {suffix!r}: {data[-200:]!r}")
        data.extend(chunk)
        if len(data) > 65536:
            raise AssertionError("Unbounded terminal output")
    return bytes(data)


def tcp_journey(ports):
    with socket.create_connection(("127.0.0.1", ports["tcp"]), timeout=5) as sock:
        intro = until(sock, b"> ")
        check(b"SSH PRIVATE KEY" in intro and b"ROOT SYSTEM ACCESS" in intro,
              "Installed adventure advertises its recovery objective")
        route = ["east", "take access_crystal", "west", "north", "west",
                 "take security_seal", "north", "east", "use security_seal",
                 "the one", "north", "use root_console"]
        for index, value in enumerate(route):
            sock.sendall(value.encode() + b"\n")
            suffix = b"We live for... we die for... > " if index == 8 else b"> "
            if index == len(route) - 1:
                suffix = b"root@g-sector-lwr:~# "
            until(sock, suffix)
        sock.sendall(b"cat /root/.ssh/id_ed25519\n")
        result = until(sock, b"root@g-sector-lwr:~# ")
        check(b"BEGIN OPENSSH PRIVATE KEY" in result, "Installed service completes journey and returns decoy key")
        sock.sendall(b"quit\n")


def tls_and_ssh(stage, ports):
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE  # The fixture deliberately uses a self-signed certificate.
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    with socket.create_connection(("127.0.0.1", ports["tls"]), timeout=5) as raw:
        with context.wrap_socket(raw, server_hostname="station.invalid") as tls:
            check(b"ROOT SYSTEM ACCESS" in until(tls, b"> "), "TLS service serves the adventure")
            tls.sendall(b"quit\n")
    key = stage / "client-key"
    command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(key))
    args = ("ssh", "-F", "/dev/null", "-T", "-p", str(ports["ssh"]), "-i", str(key),
            "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes",
            "-o", "StrictHostKeyChecking=accept-new", "-o", f"UserKnownHostsFile={stage / 'known-hosts'}",
            "-o", "GlobalKnownHostsFile=/dev/null", "visitor@127.0.0.1")
    result = command(*args, "cat /etc/passwd")
    check("don't see" in result.stdout and "root:x:" not in result.stdout,
          "SSH exec enters the adventure and cannot read host files")


def permissions():
    for unit, account in (("mudpot.service", "mudpot"), ("mudpot-dashboard.service", "mudpot-dashboard")):
        pid = int(prop(unit, "MainPID"))
        status = dict(line.split(":", 1) for line in Path(f"/proc/{pid}/status").read_text().splitlines() if ":" in line)
        check(int(status["Uid"].split()[0]) == pwd.getpwnam(account).pw_uid,
              f"{unit} runs as its dedicated unprivileged user")
        check(status["NoNewPrivs"].strip() == "1" and int(status["CapEff"].strip(), 16) == 0,
              f"{unit} has no effective capabilities and cannot gain privileges")
        probe = command("nsenter", "-t", str(pid), "-m", "--", "python3", "-c",
                        "open('/opt/mudpot/write-probe','x').close()", ok=False)
        check(probe.returncode != 0 and "Read-only file system" in probe.stderr,
              f"{unit} mount namespace enforces a read-only application directory")
    for name, mode in (("/var/lib/mudpot/state", 0o700), ("/var/lib/mudpot/state/ssh_host_ed25519", 0o600),
                       ("/var/lib/mudpot/state/tls_key.pem", 0o600), ("/var/log/mudpot", 0o2750),
                       ("/var/log/mudpot/events.jsonl", 0o640)):
        check(stat.S_IMODE(os.stat(name).st_mode) == mode, f"Permissions {oct(mode)} on {name}")
    log = "/var/log/mudpot/events.jsonl"
    check(command("runuser", "-u", "mudpot-dashboard", "--", "test", "-r", log, ok=False).returncode == 0,
          "Dashboard account can read honeypot logs")
    for flag, name in (("-w", log), ("-r", "/var/lib/mudpot/state/ssh_host_ed25519"),
                       ("-r", "/etc/mudpot/honeypot.env")):
        check(command("runuser", "-u", "mudpot-dashboard", "--", "test", flag, name, ok=False).returncode != 0,
              f"Dashboard account denied {flag} {name}")
    check(command("runuser", "-u", "mudpot", "--", "test", "-r", "/etc/mudpot/dashboard.env", ok=False).returncode != 0,
          "Honeypot account cannot read dashboard credentials")


def dashboard_checks(ports):
    check(request(ports, token="")[0] == 401, "Dashboard requires authentication")
    check(request(ports, token="wrong")[0] == 401, "Dashboard rejects incorrect credentials")
    status, body = request(ports)
    check(status == 200 and "vault recovered" in body, "Separate dashboard observes honeypot victory")
    check(request(ports, method="POST")[0] == 405, "Dashboard rejects mutations")
    check(request(ports, path="/.env")[0] == 404, "Dashboard cannot serve environment files")
    check(request(ports, host="attacker.invalid")[0] == 403, "Dashboard rejects unexpected Host headers")
    conn = http.client.HTTPConnection("127.0.0.1", ports["tcp"], timeout=5)
    conn.request("GET", "/dashboard")
    response = conn.getresponse()
    public = response.read().decode()
    conn.close()
    check(response.status == 200 and "Administrator Recovery" in public and "Private Monitor" not in public
          and "mudpot-service-test-token" not in public, "Public listener never serves the private dashboard or token")


def hashes():
    names = ["/etc/mudpot/honeypot.env", "/etc/mudpot/dashboard.env",
             "/opt/mudpot/adventures/grey-sector/adventure.json",
             "/var/lib/mudpot/state/ssh_host_ed25519", "/var/lib/mudpot/state/tls_key.pem"]
    return {name: hashlib.sha256(Path(name).read_bytes()).hexdigest() for name in names}


def crash_restart(ports):
    old = prop("mudpot.service", "MainPID")
    command("systemctl", "kill", "--signal=SIGKILL", "mudpot.service")
    wait_for(lambda: prop("mudpot.service", "ActiveState") == "active"
             and prop("mudpot.service", "MainPID") not in (old, "0"), "Honeypot did not recover after crash")
    ready(ports)
    check(int(prop("mudpot.service", "NRestarts")) >= 1, "systemd restarts a crashed honeypot")
    check(request(ports)[0] == 200, "Dashboard remains available across honeypot restart")


def rotate(ports):
    command("systemctl", "stop", "mudpot.service")
    check(prop("mudpot.service", "ExecMainStatus") == "0", "Honeypot exits cleanly on SIGTERM")
    # Prime valid records while the writer is stopped. Never race a second log writer.
    log = Path("/var/log/mudpot/events.jsonl")
    record = (json.dumps({"time": "2026-10-22T00:00:00Z", "session": "rotation-fixture", "ip": "192.0.2.1",
                          "kind": "probe", "text": "x" * 800}) + "\n").encode()
    with log.open("ab") as stream:
        remaining = 10 * 1024 * 1024 - log.stat().st_size
        stream.write(record * (remaining // len(record)))
    command("systemctl", "start", "mudpot.service")
    ready(ports)

    def read_dashboard(_):
        for _ in range(4):
            status, _ = request(ports)
            if status not in (200, 503):  # The bounded handler can reject excess simultaneous renders.
                raise AssertionError(f"Unexpected dashboard response {status}")

    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        readers = [pool.submit(read_dashboard, i) for i in range(4)]
        for _ in range(4):
            with socket.create_connection(("127.0.0.1", ports["tcp"]), timeout=5) as sock:
                sock.sendall(b"look\nquit\n")
                while sock.recv(4096):
                    pass
        for reader in readers:
            reader.result()
    wait_for(lambda: Path(str(log) + ".1").exists(), "Log did not rotate")
    check(stat.S_IMODE(log.stat().st_mode) == 0o640, "Rotated writer preserves private log permissions")
    check(request(ports)[0] == 200, "Dashboard reads logs successfully after live rotation")


def concurrent_clients(ports):
    def public(index):
        try:
            with socket.create_connection(("127.0.0.1", ports["tcp"]), timeout=5) as sock:
                sock.sendall(b"look\ninventory\nquit\n")
                while sock.recv(4096):
                    pass
        except (ConnectionResetError, BrokenPipeError):
            pass  # The fixture intentionally exceeds the four-connections-per-IP cap.

    def private(index):
        status, _ = request(ports)
        check(status in (200, 503), f"Concurrent dashboard request {index} is served or bounded")

    with concurrent.futures.ThreadPoolExecutor(max_workers=12) as pool:
        futures = [pool.submit(public, i) for i in range(24)]
        futures += [pool.submit(private, i) for i in range(8)]
        for future in futures:
            future.result()
    check(prop("mudpot.service", "ActiveState") == "active" and prop("mudpot-dashboard.service", "ActiveState") == "active",
          "Both instrumented services survive concurrent clients")
    for unit in ("mudpot.service", "mudpot-dashboard.service"):
        check(prop(unit, "NRestarts") == "0", f"Instrumented {unit} did not crash or report a race")


def main():
    phase, directory = sys.argv[1:]
    stage = Path(directory)
    if phase == "configure":
        configure(stage)
        return
    ports = json.loads((stage / "ports.json").read_text())
    ready(ports)
    if phase == "production":
        tcp_journey(ports)
        tls_and_ssh(stage, ports)
        permissions()
        dashboard_checks(ports)
        crash_restart(ports)
        rotate(ports)
        manifest_path = Path("/opt/mudpot/adventures/grey-sector/adventure.json")
        manifest = json.loads(manifest_path.read_text())
        manifest["responses"]["operator_probe"] = "Operator customization preserved."
        manifest_path.write_text(json.dumps(manifest) + "\n")
        (stage / "preserved.json").write_text(json.dumps(hashes()))
    elif phase == "upgrade":
        check(hashes() == json.loads((stage / "preserved.json").read_text()),
              "Repeat deployment preserves operator settings, adventure, and transport keys")
        with socket.create_connection(("127.0.0.1", ports["tcp"]), timeout=5) as sock:
            until(sock, b"> ")
            sock.sendall(b"operator_probe\n")
            check(b"Operator customization preserved." in until(sock, b"> "),
                  "Upgraded service loads the operator's customized adventure")
            sock.sendall(b"quit\n")
        permissions()
    elif phase == "race":
        permissions()
        concurrent_clients(ports)
        # Keep incomplete handshakes open while systemd delivers SIGTERM.
        held = [socket.create_connection(("127.0.0.1", ports[kind]), timeout=5) for kind in ("tcp", "ssh", "tls")]
        try:
            command("systemctl", "stop", "mudpot.service", "mudpot-dashboard.service")
        finally:
            for sock in held:
                sock.close()
        for unit in ("mudpot.service", "mudpot-dashboard.service"):
            check(prop(unit, "Result") == "success" and prop(unit, "ExecMainStatus") == "0",
                  f"Instrumented {unit} drains and exits cleanly on SIGTERM")
    else:
        raise AssertionError(f"Unknown phase: {phase}")


if __name__ == "__main__":
    main()
