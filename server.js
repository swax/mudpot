const net = require("net");
const tls = require("tls");
const { execSync } = require("child_process");
const fs = require("fs");
const path = require("path");
const log = require("./log");
const { createSession, look, handleInput } = require("./game");
const handleScanner = require("./scanner");
const startSSH = require("./ssh");

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

function getTlsCert() {
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

function onConnection(socket) {
  if (connections.count >= MAX_CONNECTIONS) {
    socket.end("The lower levels are too crowded tonight. Try again later.\n");
    return;
  }
  connections.count++;

  const ip = (socket.remoteAddress || "unknown").replace("::ffff:", "");
  const port = socket.localPort;
  const session = createSession(ip);
  const scannerState = { identified: false, mode: null };

  log(ip, `CONNECTED port=${port}`);

  socket.write(BANNER);
  socket.write(look(session));
  socket.write("\n> ");

  socket.setTimeout(60000); // 1 min idle timeout

  let buffer = "";
  let cmdCount = 0;
  let cmdWindowStart = Date.now();
  socket.on("data", (data) => {
    if (handleScanner(data, ip, socket, scannerState)) return;

    // Handle Ctrl+C (raw 0x03), Ctrl+D (0x04), or telnet IAC IP (0xFF 0xF4)
    if (data.includes(0x03) || data.includes(0x04) || data.includes(0xf4)) {
      log(ip, "QUIT");
      socket.end(
        "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
      );
      return;
    }

    buffer += data.toString();

    // Drop connection if buffer grows too large (no newline flood)
    if (buffer.length > MAX_BUFFER) {
      log(ip, "KICKED: buffer overflow");
      socket.end("\nConnection closed.\n");
      return;
    }

    let newlineIdx;
    while ((newlineIdx = buffer.indexOf("\n")) !== -1) {
      let line = buffer.substring(0, newlineIdx).replace(/\r/g, "");
      buffer = buffer.substring(newlineIdx + 1);

      // Handle Ctrl+C that arrived as part of a line
      if (line.includes("\x03")) {
        log(ip, "QUIT");
        socket.end(
          "\nYou find your way back to the upper levels. The station hums quietly behind you.\n",
        );
        return;
      }

      // Truncate long lines
      if (line.length > MAX_LINE) {
        line = line.substring(0, MAX_LINE);
      }

      // Rate limit commands
      const now = Date.now();
      if (now - cmdWindowStart > 60000) {
        cmdCount = 0;
        cmdWindowStart = now;
      }
      cmdCount++;
      if (cmdCount > MAX_CMDS_PER_MIN) {
        log(ip, "KICKED: rate limit");
        socket.end(
          "\nThe station does not respond well to haste. Connection closed.\n",
        );
        return;
      }

      const response = handleInput(session, line);
      if (response === "QUIT") {
        log(ip, "QUIT");
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
        },
        500 + Math.floor(Math.random() * 1000),
      );
    }
  });

  socket.on("timeout", () => {
    log(ip, "TIMEOUT");
    socket.end(
      "\nYou drift off to sleep in a forgotten corridor. The station continues without you.\n",
    );
  });

  socket.on("close", () => {
    connections.count--;
    log(
      ip,
      `DISCONNECTED (visited: ${session.room}, inventory: [${session.inventory.join(", ")}])`,
    );
  });

  socket.on("error", (err) => {
    log(ip, `ERROR: ${err.message}`);
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
