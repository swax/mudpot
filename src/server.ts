import net from "net";
import tls from "tls";
import { execSync } from "child_process";
import fs from "fs";
import path from "path";
import log from "./log";
import { createSession, look, handleInput } from "./game";
import handleScanner from "./scanner";
import startSSH from "./ssh";
import { ScannerState } from "./types";

const BANNER = `
\x1b[1;34m╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║              G R E Y    S E C T O R                          ║
║                                                              ║
║         Babylon Station  //  Lower Levels                    ║
║                                                              ║
║         The station sleeps. You are still awake.             ║
║         The lower levels stretch out before you.             ║
║                                                              ║
║         Type 'help' for commands.                            ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝\x1b[0m
`;

const PORTS = (process.env.MUDPOT_PORT || "2222")
  .split(",")
  .map((p) => parseInt(p.trim(), 10));
const TLS_PORTS = (process.env.MUDPOT_TLS_PORT || "")
  .split(",")
  .map((p) => parseInt(p.trim(), 10))
  .filter((p) => !isNaN(p));
const MAX_CONNECTIONS = 20;
const MAX_BUFFER = 4096;
const MAX_LINE = 256;
const MAX_CMDS_PER_MIN = 60;
const connections = { count: 0 };

function getTlsCert(): { cert: Buffer; key: Buffer } {
  const certPath = path.join(__dirname, "tls_cert.pem");
  const keyPath = path.join(__dirname, "tls_key.pem");
  try {
    return {
      cert: fs.readFileSync(certPath),
      key: fs.readFileSync(keyPath),
    };
  } catch {
    execSync(
      `openssl req -x509 -newkey rsa:2048 -keyout "${keyPath}" -out "${certPath}" -days 3650 -nodes -subj "/CN=BabylonStation"`,
    );
    fs.chmodSync(keyPath, 0o600);
    return {
      cert: fs.readFileSync(certPath),
      key: fs.readFileSync(keyPath),
    };
  }
}

function onConnection(socket: net.Socket): void {
  if (connections.count >= MAX_CONNECTIONS) {
    socket.end("The lower levels are too crowded tonight. Try again later.\n");
    return;
  }
  connections.count++;

  const ip = (socket.remoteAddress || "unknown").replace("::ffff:", "");
  const connId = `${ip}:${socket.remotePort}`;
  const port = socket.localPort;
  const session = createSession(connId);
  const scannerState: ScannerState = { identified: false, mode: null };

  log(connId, `CONNECTED port=${port}`);

  // Wait briefly for the client to speak first so we can identify
  // non-MUD protocols (HTTP, FTP, SSH, TLS) before sending the banner.
  // Scanners send data immediately; MUD/telnet clients wait for the server.
  let greeted = false;
  const greetTimer = setTimeout(() => {
    if (!greeted && !socket.destroyed) {
      greeted = true;
      socket.write(BANNER);
      socket.write(look(session));
      socket.write("\n> ");
    }
  }, 500);

  socket.setTimeout(60000); // 1 min idle timeout

  let buffer = "";
  let cmdCount = 0;
  let cmdWindowStart = Date.now();
  const cmdQueue: string[] = [];
  let processingCmd = false;

  function processNext(): void {
    if (processingCmd || cmdQueue.length === 0 || socket.destroyed) return;
    processingCmd = true;

    const line = cmdQueue.shift()!;

    const response = handleInput(session, line);
    if (response === "QUIT") {
      log(connId, "QUIT");
      socket.end(
        "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
      );
      return;
    }

    if (response.startsWith("VICTORY")) {
      const text = response.slice("VICTORY".length);
      setTimeout(
        () => {
          if (!socket.destroyed) socket.end(text);
        },
        500 + Math.floor(Math.random() * 1000),
      );
      return;
    }

    // Delay response — feels like the station is thinking
    setTimeout(
      () => {
        if (socket.destroyed) return;
        socket.write(response);
        if (session.inputMode !== "puzzle") {
          socket.write("> ");
        }
        processingCmd = false;
        processNext();
      },
      500 + Math.floor(Math.random() * 1000),
    );
  }

  socket.on("data", (data: Buffer) => {
    if (handleScanner(data, connId, socket, scannerState)) {
      clearTimeout(greetTimer);
      return;
    }

    // Not a known protocol - greet as MUD client if we haven't yet
    if (!greeted) {
      greeted = true;
      clearTimeout(greetTimer);
      socket.write(BANNER);
      socket.write(look(session));
      socket.write("\n> ");
    }

    // Strip telnet IAC sequences before checking for control characters.
    // IAC sequences are 0xFF followed by a command byte (and sometimes an
    // option byte). Without stripping, option bytes like 0x03 or 0x04
    // (Suppress-Go-Ahead, Approx-Message-Size) trigger false quit detection.
    const stripped = Buffer.from(
      Array.from(data).filter((_, i) => {
        if (data[i - 1] === 0xff) return false; // command byte after IAC
        if (data[i - 2] === 0xff && data[i - 2 + 1] >= 0xfa && data[i - 2 + 1] <= 0xfe)
          return false; // option byte after WILL/WONT/DO/DONT/SB
        if (data[i] === 0xff && i + 1 < data.length) return false; // IAC byte itself
        return true;
      }),
    );

    // Handle Ctrl+C (raw 0x03), Ctrl+D (0x04), or telnet IAC IP (0xFF 0xF4)
    if (stripped.includes(0x03) || stripped.includes(0x04)) {
      log(connId, "QUIT");
      socket.end(
        "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
      );
      return;
    }
    // Check for IAC IP (0xFF 0xF4) in the original data as a proper 2-byte sequence
    for (let i = 0; i < data.length - 1; i++) {
      if (data[i] === 0xff && data[i + 1] === 0xf4) {
        log(connId, "QUIT");
        socket.end(
          "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
        );
        return;
      }
    }

    buffer += stripped.toString();

    // Drop connection if buffer grows too large (no newline flood)
    if (buffer.length > MAX_BUFFER) {
      log(connId, "KICKED: buffer overflow");
      socket.end("\nConnection closed.\n");
      return;
    }

    let newlineIdx;
    while ((newlineIdx = buffer.indexOf("\n")) !== -1) {
      let line = buffer.substring(0, newlineIdx).replace(/\r/g, "");
      buffer = buffer.substring(newlineIdx + 1);

      // Handle Ctrl+C that arrived as part of a line
      if (line.includes("\x03")) {
        log(connId, "QUIT");
        socket.end(
          "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
        );
        return;
      }

      // Truncate long lines
      if (line.length > MAX_LINE) {
        line = line.substring(0, MAX_LINE);
      }

      // Rate limit on arrival, not on processing
      const now = Date.now();
      if (now - cmdWindowStart > 60000) {
        cmdCount = 0;
        cmdWindowStart = now;
      }
      cmdCount++;
      if (cmdCount > MAX_CMDS_PER_MIN) {
        log(connId, "KICKED: rate limit");
        socket.end(
          "\nThe station does not respond well to haste. Connection closed.\n",
        );
        return;
      }

      cmdQueue.push(line);
    }
    processNext();
  });

  socket.on("timeout", () => {
    log(connId, "TIMEOUT");
    socket.end(
      "\nYou drift off to sleep in a forgotten corridor. The station continues without you.\n",
    );
  });

  socket.on("close", () => {
    clearTimeout(greetTimer);
    connections.count--;
    log(
      connId,
      `DISCONNECTED (visited: ${session.room}, inventory: [${session.inventory.join(", ")}])`,
    );
  });

  socket.on("error", (err: Error) => {
    log(connId, `ERROR: ${err.message}`);
  });
}

PORTS.forEach((port) => {
  net.createServer(onConnection).listen(port, () => {
    console.log(`Grey Sector listening on port ${port}`);
    log("SYSTEM", `Server started on port ${port}`);
  });
});

if (TLS_PORTS.length > 0) {
  const tlsCert = getTlsCert();
  TLS_PORTS.forEach((port) => {
    tls.createServer(tlsCert, onConnection).listen(port, () => {
      console.log(`Grey Sector TLS listening on port ${port}`);
      log("SYSTEM", `TLS server started on port ${port}`);
    });
  });
}

startSSH({
  banner: BANNER,
  connections,
  maxConnections: MAX_CONNECTIONS,
  maxLine: MAX_LINE,
  maxCmdsPerMin: MAX_CMDS_PER_MIN,
  idleTimeout: 60000,
});
