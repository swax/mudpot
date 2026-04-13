export interface Room {
  name: string;
  desc: string;
  exits: Record<string, string>;
  locked?: Record<string, string>;
  lockItem?: string;
  unlockMsg?: string;
  puzzle?: boolean;
  puzzleAnswer?: string;
  puzzleSolved?: boolean;
  puzzlePrompt?: string;
  puzzleSuccess?: string;
  puzzleFail?: string;
  pickup?: string;
  items: Record<string, string>;
}

export interface Session {
  room: string;
  inventory: string[];
  ip: string;
  inputMode: "normal" | "puzzle";
  rooms: Record<string, Room>;
}

export interface ScannerState {
  identified: boolean;
  mode: string | null;
}

export interface SSHConfig {
  banner: string;
  connections: { count: number };
  maxConnections: number;
  maxLine: number;
  maxCmdsPerMin: number;
  idleTimeout: number;
}
