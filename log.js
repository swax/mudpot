const fs = require('fs');

const LOG_FILE = process.env.MUDPOT_LOG || '/var/log/mudpot.log';
const MAX_LOG_SIZE = 10 * 1024 * 1024; // 10MB

function log(ip, msg) {
  try {
    const stats = fs.statSync(LOG_FILE);
    if (stats.size > MAX_LOG_SIZE) {
      fs.renameSync(LOG_FILE, LOG_FILE + '.1');
    }
  } catch (e) {}
  const line = `${new Date().toISOString()} [${ip}] ${msg}\n`;
  fs.appendFileSync(LOG_FILE, line);
}

module.exports = log;
