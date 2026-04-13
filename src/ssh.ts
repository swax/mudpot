import crypto from "crypto";
import fs from "fs";
import path from "path";
import { Server as SSH2Server, Connection, ServerChannel } from "ssh2";
import log from "./log";
import { createSession, look, handleInput } from "./game";
import { Session, SSHConfig } from "./types";

const SSH_PORTS = (process.env.MUDPOT_SSH_PORT || "2223")
  .split(",")
  .map((p) => parseInt(p.trim(), 10));

function getHostKey(): string | Buffer {
  const keyPath = path.join(__dirname, "host_key");
  try {
    return fs.readFileSync(keyPath);
  } catch {
    const { privateKey } = crypto.generateKeyPairSync("rsa", {
      modulusLength: 2048,
      privateKeyEncoding: { type: "pkcs1", format: "pem" },
      publicKeyEncoding: { type: "pkcs1", format: "pem" },
    });
    fs.writeFileSync(keyPath, privateKey, { mode: 0o600 });
    return privateKey;
  }
}

function sshWrite(stream: ServerChannel, text: string): void {
  if (!stream.destroyed) stream.write(text.replace(/\r?\n/g, "\r\n"));
}

function startSSH({
  banner,
  connections,
  maxConnections,
  maxLine,
  maxCmdsPerMin,
  idleTimeout,
}: SSHConfig): void {
  const hostKey = getHostKey();

  function onSSHConnection(client: Connection, port: number): void {
    const sock = (client as unknown as { _sock?: net.Socket })._sock;
    const ip = (sock?.remoteAddress || "unknown").replace("::ffff:", "");
    const connId = `${ip}:${sock?.remotePort}`;
    let counted = false;
    let session: Session | null = null;
    let activeStream: ServerChannel | null = null;

    client.on("authentication", (ctx) => {
      const creds =
        ctx.method === "password"
          ? ` pass=${(ctx as unknown as { password: string }).password}`
          : "";
      log(connId, `SSH AUTH method=${ctx.method} user=${ctx.username}${creds}`);
      ctx.accept();
    });

    client.on("ready", () => {
      if (connections.count >= maxConnections) {
        client.end();
        return;
      }
      connections.count++;
      counted = true;
      session = createSession(connId);
      log(connId, `SSH CONNECTED port=${port}`);

      if (sock && idleTimeout) {
        sock.setTimeout(idleTimeout);
        sock.on("timeout", () => {
          log(connId, "SSH TIMEOUT");
          if (activeStream && !activeStream.destroyed) {
            sshWrite(
              activeStream,
              "\nYou drift off to sleep in a forgotten corridor. The station continues without you.\n",
            );
          }
          client.end();
        });
      }

      client.on("session", (accept) => {
        const sshSession = accept();

        sshSession.on("pty", (accept) => {
          accept();
        });

        sshSession.on("shell", (accept) => {
          const stream = accept();
          activeStream = stream;

          sshWrite(stream, banner);
          sshWrite(stream, look(session!));
          sshWrite(stream, "\n> ");

          let lineBuffer = "";
          let cmdCount = 0;
          let cmdWindowStart = Date.now();
          let escapeState = 0; // 0=normal, 1=saw ESC, 2=saw ESC[

          stream.on("data", (data: Buffer) => {
            for (let i = 0; i < data.length; i++) {
              const ch = data[i];

              // Consume escape sequences (arrow keys, function keys, etc.)
              if (escapeState === 1) {
                escapeState = ch === 0x5b ? 2 : 0;
                continue;
              }
              if (escapeState === 2) {
                if (ch >= 0x40 && ch <= 0x7e) escapeState = 0;
                continue;
              }
              if (ch === 0x1b) {
                escapeState = 1;
                continue;
              }

              // Ctrl+C / Ctrl+D
              if (ch === 0x03 || ch === 0x04) {
                log(connId, "SSH QUIT");
                sshWrite(
                  stream,
                  "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
                );
                stream.end();
                client.end();
                return;
              }

              // Enter
              if (ch === 0x0d) {
                stream.write("\r\n");

                const line = lineBuffer.substring(0, maxLine);
                lineBuffer = "";

                const now = Date.now();
                if (now - cmdWindowStart > 60000) {
                  cmdCount = 0;
                  cmdWindowStart = now;
                }
                cmdCount++;
                if (cmdCount > maxCmdsPerMin) {
                  log(connId, "SSH KICKED: rate limit");
                  sshWrite(
                    stream,
                    "\nThe station does not respond well to haste. Connection closed.\n",
                  );
                  stream.end();
                  client.end();
                  return;
                }

                const response = handleInput(session!, line);
                if (response === "QUIT") {
                  log(connId, "SSH QUIT");
                  sshWrite(
                    stream,
                    "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
                  );
                  stream.end();
                  client.end();
                  return;
                }

                if (response.startsWith("VICTORY")) {
                  const text = response.slice("VICTORY".length);
                  setTimeout(() => {
                    sshWrite(stream, text);
                    stream.end();
                    client.end();
                  }, 500 + Math.floor(Math.random() * 1000));
                  return;
                }

                setTimeout(() => {
                  if (stream.destroyed) return;
                  sshWrite(stream, response);
                  if (session!.inputMode !== "puzzle") {
                    stream.write("> ");
                  }
                }, 500 + Math.floor(Math.random() * 1000));
                continue;
              }

              // Backspace / Delete
              if (ch === 0x7f || ch === 0x08) {
                if (lineBuffer.length > 0) {
                  lineBuffer = lineBuffer.slice(0, -1);
                  stream.write("\b \b");
                }
                continue;
              }

              // Ignore other control characters
              if (ch < 0x20) continue;

              // Buffer limit
              if (lineBuffer.length >= maxLine) continue;

              // Printable character — echo and buffer
              lineBuffer += String.fromCharCode(ch);
              stream.write(String.fromCharCode(ch));
            }
          });
        });

        sshSession.on("exec", (accept, _reject, info) => {
          log(connId, `SSH EXEC: ${info.command}`);
          if (!session) session = createSession(connId);
          const response = handleInput(session, info.command);
          const stream = accept();
          if (response === "QUIT") {
            sshWrite(stream, "\nConnection closed.\n");
          } else if (response.startsWith("VICTORY")) {
            sshWrite(stream, response.slice("VICTORY".length));
          } else if (response) {
            sshWrite(stream, response);
          }
          stream.exit(0);
          stream.end();
        });

        sshSession.on("subsystem", (accept, _reject, info) => {
          log(connId, `SSH SUBSYSTEM: ${info.name}`);
          const stream = accept();
          sshWrite(
            stream,
            "That subsystem is not available in Grey Sector.\n",
          );
          stream.exit(1);
          stream.end();
        });
      });
    });

    client.on("close", () => {
      if (counted) {
        connections.count--;
        log(
          connId,
          `SSH DISCONNECTED (visited: ${session!.room}, inventory: [${session!.inventory.join(", ")}])`,
        );
      }
    });

    client.on("error", (err) => {
      log(connId, `SSH ERROR: ${err.message}`);
    });
  }

  SSH_PORTS.forEach((port) => {
    new SSH2Server({ hostKeys: [hostKey] }, (client) => {
      onSSHConnection(client, port);
    }).listen(port, () => {
      console.log(`Grey Sector SSH listening on port ${port}`);
      log("SYSTEM", `SSH server started on port ${port}`);
    });
  });
}

export default startSSH;

// net is used only for the type of _sock
import net from "net";
