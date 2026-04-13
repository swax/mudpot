import fs from "fs";

const MAX_LOG_SIZE = 10 * 1024 * 1024; // 10MB

function log(ip: string, msg: string): void {
  const logFile = process.env.MUDPOT_LOG || "";
  if (!logFile) return;
  
  try {
    const stats = fs.statSync(logFile);
    if (stats.size > MAX_LOG_SIZE) {
      fs.renameSync(logFile, logFile + ".1");
    }
  } catch (_e) {
    // File may not exist yet, that's fine
  }
  const line = `${new Date().toISOString()} [${ip}] ${msg}\n`;
  fs.appendFileSync(logFile, line);
}

export default log;
