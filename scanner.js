const log = require('./log');

// Detect non-MUD protocols and handle them.
// Returns true if the data was handled (caller should skip MUD processing).
function handleScanner(data, ip, socket, state) {
  if (!state.identified) {
    state.identified = true;
    const probe = data.toString().split('\n')[0].replace(/\r/g, '').trim();

    // TLS or other binary — they can't talk to a dungeon in ciphertext
    if (data[0] === 0x16 && data[1] === 0x03) {
      log(ip, 'SCANNER:TLS');
      socket.end('The dungeon does not speak in tongues. Try knocking normally.\n');
      return true;
    }
    // SSH — fake banner, they'll fail key exchange and leave
    if (probe.startsWith('SSH-2.0-')) {
      state.mode = 'ssh';
      log(ip, `SCANNER:SSH ${probe}`);
      socket.write('SSH-2.0-MudPotOS_4.2.0 You_rolled_a_1_on_your_hacking_check\r\n');
      return true;
    }
    // HTTP — serve the lobby as a web page
    if (/^(GET|POST|HEAD|PUT|DELETE|OPTIONS)\s/.test(probe)) {
      log(ip, `SCANNER:HTTP ${probe}`);
      const html = `<html><head><title>Server Lobby</title></head><body>
<h1>You are in the Server Lobby</h1>
<p>The air hums with cooling fans. A sign reads: AUTHORIZED PERSONNEL ONLY.</p>
<p>A README.txt on the desk says: "Database credentials moved to the vault. Keycard required."</p>
<ul><li><a href="/corridor">North: Dim Corridor</a></li><li><a href="/storage">East: Storage Closet</a></li></ul>
<form method="POST" action="/terminal"><input name="cmd" placeholder="Enter command..."><button>Submit</button></form>
</body></html>`;
      socket.end(`HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: ${Buffer.byteLength(html)}\r\n\r\n${html}`);
      return true;
    }
    // FTP — play along with MUD-themed responses
    if (/^USER\s/i.test(probe)) {
      state.mode = 'ftp';
      log(ip, `SCANNER:FTP ${probe}`);
      socket.write('331 The dungeon keeper requires a password.\r\n');
      return true;
    }
    // Known dead probes (MGLNDD taggers, etc)
    if (/^MGLNDD_/.test(probe)) {
      log(ip, `SCANNER:PROBE ${probe}`);
      socket.end('You cast IDENTIFY but the dungeon resists. It stares back.\n');
      return true;
    }
  }

  // Handle ongoing non-MUD sessions
  if (state.mode) {
    const line = data.toString().split('\n')[0].replace(/\r/g, '').trim();
    if (state.mode === 'ftp') {
      log(ip, `SCANNER:FTP ${line}`);
      if (/^PASS\s/i.test(line)) socket.write('230 Welcome, adventurer. You are in the Server Lobby. Try LIST.\r\n');
      else if (/^LIST/i.test(line)) socket.write('150 Opening connection.\r\ndrwxr-xr-x 2 root root 4096 .ssh/\r\n-rw-r--r-- 1 root root  217 README.txt\r\n-rw------- 1 root root   42 .vault_key\r\n226 Transfer complete.\r\n');
      else if (/^RETR/i.test(line)) socket.write('150 Opening connection.\r\nCredentials moved to the vault. Keycard required.\r\n226 Transfer complete.\r\n');
      else if (/^PWD/i.test(line)) socket.write('257 "/server/lobby"\r\n');
      else if (/^CWD/i.test(line)) socket.write('250 You move deeper into the server.\r\n');
      else if (/^SYST/i.test(line)) socket.write('215 UNIX Type: MudPotOS\r\n');
      else if (/^QUIT/i.test(line)) socket.end('221 Your session has been logged. Have a nice day.\r\n');
      else socket.write('500 Unknown command. Try LIST, RETR, or PWD.\r\n');
    }
    return true;
  }

  return false;
}

module.exports = handleScanner;
