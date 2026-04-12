const net = require('net');
const log = require('./log');
const { createSession, look, handleInput } = require('./game');
const handleScanner = require('./scanner');

const BANNER = `
\x1b[1;32m╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║   ███╗   ███╗██╗   ██╗██████╗ ██████╗  ██████╗ ████████╗     ║
║   ████╗ ████║██║   ██║██╔══██╗██╔══██╗██╔═══██╗╚══██╔══╝     ║
║   ██╔████╔██║██║   ██║██║  ██║██████╔╝██║   ██║   ██║        ║
║   ██║╚██╔╝██║██║   ██║██║  ██║██╔═══╝ ██║   ██║   ██║        ║
║   ██║ ╚═╝ ██║╚██████╔╝██████╔╝██║     ╚██████╔╝   ██║        ║
║   ╚═╝     ╚═╝ ╚═════╝ ╚═════╝ ╚═╝      ╚═════╝    ╚═╝        ║
║                                                              ║
║         Welcome to the server. You look tired.               ║
║         Why not stay a while and explore?                    ║
║                                                              ║
║         Type 'help' for commands.                            ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝\x1b[0m
`;

const PORT = process.env.MUDPOT_PORT || 2222;
const MAX_CONNECTIONS = 20;
let connectionCount = 0;

const server = net.createServer((socket) => {
  if (connectionCount >= MAX_CONNECTIONS) {
    socket.end('Server full. Try again later.\n');
    return;
  }
  connectionCount++;

  const ip = (socket.remoteAddress || 'unknown').replace('::ffff:', '');
  const session = createSession(ip);
  const scannerState = { identified: false, mode: null };

  log(ip, 'CONNECTED');

  socket.write(BANNER);
  socket.write(look(session));
  socket.write('\n> ');

  socket.setTimeout(300000); // 5 min idle timeout

  let buffer = '';
  let cmdCount = 0;
  let cmdWindowStart = Date.now();
  const MAX_BUFFER = 4096;
  const MAX_LINE = 256;
  const MAX_CMDS_PER_MIN = 60;

  socket.on('data', (data) => {
    if (handleScanner(data, ip, socket, scannerState)) return;

    buffer += data.toString();

    // Drop connection if buffer grows too large (no newline flood)
    if (buffer.length > MAX_BUFFER) {
      log(ip, 'KICKED: buffer overflow');
      socket.end('\nConnection closed.\n');
      return;
    }

    let newlineIdx;
    while ((newlineIdx = buffer.indexOf('\n')) !== -1) {
      let line = buffer.substring(0, newlineIdx).replace(/\r/g, '');
      buffer = buffer.substring(newlineIdx + 1);

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
        log(ip, 'KICKED: rate limit');
        socket.end('\nSlow down. Connection closed.\n');
        return;
      }

      const response = handleInput(session, line);
      if (response === 'QUIT') {
        log(ip, 'QUIT');
        socket.end('\nGoodbye. Your session has been logged. Have a nice day.\n');
        return;
      }

      // Random delay 500-1500ms — wastes time and feels realistic
      const delay = 500 + Math.floor(Math.random() * 1000);
      setTimeout(function() {
        if (socket.destroyed) return;
        socket.write(response);

        if (session.inputMode === 'puzzle') {
          // prompt already sent in response
        } else {
          socket.write('> ');
        }
      }, delay);
    }
  });

  socket.on('timeout', () => {
    log(ip, 'TIMEOUT');
    socket.end('\nYou drift off to sleep in the server room. Connection closed.\n');
  });

  socket.on('close', () => {
    connectionCount--;
    log(ip, `DISCONNECTED (visited: ${session.room}, inventory: [${session.inventory.join(', ')}])`);
  });

  socket.on('error', (err) => {
    log(ip, `ERROR: ${err.message}`);
  });
});

server.listen(PORT, () => {
  console.log(`MudPot listening on port ${PORT}`);
  log('SYSTEM', `Server started on port ${PORT}`);
});
