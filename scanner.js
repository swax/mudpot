const log = require('./log');

// Detect non-MUD protocols and handle them.
// Returns true if the data was handled (caller should skip MUD processing).
function handleScanner(data, ip, socket, state) {
  if (!state.identified) {
    state.identified = true;
    const probe = data.toString().split('\n')[0].replace(/\r/g, '').trim();

    // TLS or other binary
    if (data[0] === 0x16 && data[1] === 0x03) {
      log(ip, 'SCANNER:TLS');
      socket.end('The lower levels do not answer in that language. Try the standard channel.\n');
      return true;
    }
    // SSH
    if (probe.startsWith('SSH-2.0-')) {
      state.mode = 'ssh';
      log(ip, `SCANNER:SSH ${probe}`);
      socket.write('SSH-2.0-BabylonStation_5.0 Grey_Sector_Access\r\n');
      return true;
    }
    // HTTP — serve the junction as a web page
    if (/^(GET|POST|HEAD|PUT|DELETE|OPTIONS)\s/.test(probe)) {
      log(ip, `SCANNER:HTTP ${probe}`);
      const html = `<html><head><title>Junction G-17</title></head><body>
<h1>Junction G-17</h1>
<p>A junction where maintenance corridors meet in the lower levels. Dim emergency lighting casts a pale blue glow.</p>
<p>A posted notice reads: "Power fluctuations in Grey Sector. Access crystal required for Records."</p>
<ul><li><a href="/passage">North: Lower Passage</a></li><li><a href="/alcove">East: Maintenance Alcove</a></li></ul>
<form method="POST" action="/terminal"><input name="cmd" placeholder="Enter command..."><button>Submit</button></form>
</body></html>`;
      socket.end(`HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: ${Buffer.byteLength(html)}\r\n\r\n${html}`);
      return true;
    }
    // FTP
    if (/^USER\s/i.test(probe)) {
      state.mode = 'ftp';
      log(ip, `SCANNER:FTP ${probe}`);
      socket.write('331 Station security requires identification.\r\n');
      return true;
    }
    // Known dead probes (MGLNDD taggers, etc)
    if (/^MGLNDD_/.test(probe)) {
      log(ip, `SCANNER:PROBE ${probe}`);
      socket.end('The station does not recognize your signal. The corridor remains silent.\n');
      return true;
    }
  }

  // Handle ongoing non-MUD sessions
  if (state.mode) {
    const line = data.toString().split('\n')[0].replace(/\r/g, '').trim();
    if (state.mode === 'ftp') {
      log(ip, `SCANNER:FTP ${line}`);
      if (/^PASS\s/i.test(line)) socket.write('230 Welcome to Grey Sector. You are at Junction G-17. Try LIST.\r\n');
      else if (/^LIST/i.test(line)) socket.write('150 Opening connection.\r\ndrwxr-xr-x 2 station ops 4096 records/\r\n-rw-r--r-- 1 station ops  217 posted_notice.txt\r\n-rw------- 1 station ops   42 access_crystal.dat\r\n226 Transfer complete.\r\n');
      else if (/^RETR/i.test(line)) socket.write('150 Opening connection.\r\nPower fluctuations in Grey Sector. Access crystal required for Records.\r\n226 Transfer complete.\r\n');
      else if (/^PWD/i.test(line)) socket.write('257 "/station/grey-sector/g-17"\r\n');
      else if (/^CWD/i.test(line)) socket.write('250 You move deeper into the station.\r\n');
      else if (/^SYST/i.test(line)) socket.write('215 BabylonStation Grey Sector\r\n');
      else if (/^QUIT/i.test(line)) socket.end('221 You find your way back to the upper levels. Safe travels.\r\n');
      else socket.write('500 Unknown command. Try LIST, RETR, or PWD.\r\n');
    } else if (state.mode === 'ssh') {
      log(ip, 'SCANNER:SSH data');
      // SSH clients will fail key exchange and disconnect on their own
    }
    return true;
  }

  return false;
}

module.exports = handleScanner;
