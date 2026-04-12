const fs = require('fs');

const LOG_FILE = '/var/log/mudpot.log';
const MAX_LOG_SIZE = 10 * 1024 * 1024; // 10MB

function log(ip, msg) {
  try {
    const stats = fs.statSync(LOG_FILE);
    if (stats.size > MAX_LOG_SIZE) {
      // Rotate: keep last half
      const content = fs.readFileSync(LOG_FILE, 'utf8');
      fs.writeFileSync(LOG_FILE + '.old', content.substring(0, content.length / 2));
      fs.writeFileSync(LOG_FILE, content.substring(content.length / 2));
    }
  } catch (e) {}
  const line = `${new Date().toISOString()} [${ip}] ${msg}\n`;
  fs.appendFileSync(LOG_FILE, line);
}

module.exports = log;
