const net = require('net');
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

// ── Rooms ──────────────────────────────────────────────────────────────

const rooms = {
  lobby: {
    name: 'Server Lobby',
    desc: `You find yourself in what appears to be the entrance to a production server.
The air hums with the sound of cooling fans. A flickering monitor on the wall
displays scrolling access logs. A worn sign reads: "AUTHORIZED PERSONNEL ONLY."
A half-eaten mass of text sits on the desk labeled 'README.txt'.`,
    exits: { north: 'corridor', east: 'storage' },
    items: {
      'readme.txt': `=== INTERNAL NOTE ===
Database credentials have been moved to the vault.
New keycard required for access. Check the archive.
- sysadmin (2024-01-15)`
    }
  },

  corridor: {
    name: 'Dim Corridor',
    desc: `A long corridor stretches before you, lit by buzzing fluorescent lights.
Server racks line both walls, their LEDs blinking in hypnotic patterns.
Someone has taped a printed meme to one of the racks. It says "there is no
cloud, it's just someone else's computer." You notice a door to the west
marked "ARCHIVE" and stairs leading up.`,
    exits: { south: 'lobby', west: 'archive', up: 'lab' },
    items: {}
  },

  storage: {
    name: 'Storage Closet',
    desc: `A cramped storage closet filled with tangled ethernet cables and old
hardware. A dusty shelf holds rows of backup tapes from 2019. In the corner,
a small box catches your eye — it contains a rusty keycard with "ARCHIVE"
printed on it.`,
    exits: { west: 'lobby' },
    items: {
      'backup_tape': `BACKUP MANIFEST 2019-03-22
Contents: /var/db/users.sql, /etc/shadow.bak, /opt/app/.env
Status: CORRUPTED — do not restore`
    },
    pickup: 'keycard'
  },

  archive: {
    name: 'The Archive',
    desc: `Rows of old filing cabinets fill this cold room. Most drawers are empty,
but one is slightly ajar. The room smells like old paper and regret.
A terminal in the corner displays a blinking cursor. A heavy door to the
north has an electronic lock — it requires a security badge.`,
    exits: { east: 'corridor' },
    locked: { north: 'lab' },
    lockItem: 'keycard',
    unlockMsg: 'You swipe the keycard. The lock clicks green. The door to the north opens.',
    items: {
      'filing_cabinet': `PERSONNEL FILE — ADMIN ACCOUNT
Username: admin
Password: [REDACTED — see vault terminal]
Access Level: root
Notes: "Changed after the 2023 incident. Do NOT reuse."`
    },
    pickup: 'badge'
  },

  lab: {
    name: 'Development Lab',
    desc: `A cluttered dev lab. Whiteboards covered in architecture diagrams line the
walls. Three monitors show a Grafana dashboard with flatlined metrics — this
environment has been idle for months. A sticky note on one monitor reads:
"vault password hint: it's the cat's name backwards."
There's a door to the east with a badge reader, and stairs going down.`,
    exits: { south: 'archive', east: 'vault_door' },
    items: {
      'sticky_note': `"vault password hint: it's the cat's name backwards"
(scrawled in blue marker)`,
      'whiteboard': `SYSTEM ARCHITECTURE
┌──────────┐    ┌──────────┐    ┌──────────┐
│  nginx   │───>│  app:3000│───>│ postgres │
│ :443     │    │  node    │    │ :5432    │
└──────────┘    └──────────┘    └──────────┘
                     │
                ┌────┴─────┐
                │  redis   │
                │ :6379    │
                └──────────┘`
    }
  },

  vault_door: {
    name: 'Vault Entrance',
    desc: `A reinforced steel door with a digital keypad. Above the keypad, a small
screen reads: "ENTER PASSWORD." A security camera watches you from the ceiling.
A small cat figurine sits on a shelf next to the door. Its collar tag reads "Whiskers."`,
    exits: { west: 'lab' },
    locked: { north: 'vault' },
    lockItem: 'badge',
    unlockMsg: 'You hold the badge to the reader. It beeps. The keypad activates.',
    puzzle: true,
    puzzleAnswer: 'sreksihw',
    puzzleSolved: false,
    puzzlePrompt: 'KEYPAD> ',
    puzzleSuccess: `The vault door groans open, revealing a passage to the north.`,
    puzzleFail: 'The keypad buzzes red. Incorrect password.',
    items: {
      'cat_figurine': `A porcelain cat figurine. Its collar tag reads "Whiskers."
It looks like it's judging you.`
    }
  },

  vault: {
    name: 'The Vault',
    desc: `You've reached the vault. The room is small and cold. A single terminal
sits on a pedestal in the center, its screen glowing green. This is it.
The terminal displays:

  ╔══════════════════════════════════════════════╗
  ║  CLASSIFIED CREDENTIALS — ROOT ACCESS       ║
  ╠══════════════════════════════════════════════╣
  ║  Host: prod-db-01.internal                  ║
  ║  User: root                                 ║
  ║  Pass: ZjByX3RoM19sb2xz                    ║
  ║                                             ║
  ║  AWS_ACCESS_KEY_ID=AKIA3EXAMPLE7HONEYPOT    ║
  ║  AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDE  ║
  ║                                             ║
  ║  flag{congratulations_you_played_yourself}   ║
  ╚══════════════════════════════════════════════╝

A fortune cookie sits on the desk, unopened.`,
    exits: { south: 'vault_door' },
    items: {
      'fortune_cookie': `You crack it open. The fortune reads:
"Every second you spent here was a second you weren't hacking a real server.
Thanks for playing. We logged everything. <3"`,
      'terminal': `root@prod-db-01:~# SELECT * FROM users LIMIT 5;
+----+----------+------------------+---------------------+
| id | username | email            | last_login          |
+----+----------+------------------+---------------------+
|  1 | admin    | admin@corp.local | 2024-01-15 08:23:11 |
|  2 | jsmith   | js@corp.local    | 2024-01-14 17:45:02 |
|  3 | devops   | dev@corp.local   | 2024-01-15 09:01:44 |
|  4 | backup   | bk@corp.local    | 2023-12-01 00:00:00 |
|  5 | guest    | guest@corp.local | 2024-01-10 12:00:00 |
+----+----------+------------------+---------------------+
5 rows in set (0.003 sec)`
    }
  }
};

// ── Session ────────────────────────────────────────────────────────────

function createSession(ip) {
  return {
    room: 'lobby',
    inventory: [],
    ip,
    inputMode: 'normal',  // 'normal' or 'puzzle'
    rooms: JSON.parse(JSON.stringify(rooms)),
  };
}

function look(session) {
  const room = session.rooms[session.room];
  let text = `\n\x1b[1;33m${room.name}\x1b[0m\n${room.desc}\n`;

  const exits = Object.keys(room.exits || {});
  const locked = Object.keys(room.locked || {});
  const allExits = exits.concat(locked);
  if (allExits.length > 0) {
    text += `\n\x1b[1;36mExits:\x1b[0m ${allExits.join(', ')}`;
  }
  text += '\n';
  return text;
}

function handleInput(session, raw) {
  const input = raw.trim().toLowerCase();
  if (!input) return '';

  log(session.ip, `[${session.room}] ${raw.trim()}`);

  // Puzzle input mode
  if (session.inputMode === 'puzzle') {
    const room = session.rooms[session.room];
    if (input === room.puzzleAnswer) {
      room.puzzleSolved = true;
      // Unlock the door
      const dir = Object.keys(room.locked)[0];
      room.exits[dir] = room.locked[dir];
      delete room.locked[dir];
      session.inputMode = 'normal';
      return `\n\x1b[1;32m${room.puzzleSuccess}\x1b[0m\n\n`;
    } else if (input === 'quit' || input === 'back' || input === 'cancel') {
      session.inputMode = 'normal';
      return '\nYou step back from the keypad.\n\n';
    } else {
      return `\n\x1b[1;31m${room.puzzleFail}\x1b[0m\n${room.puzzlePrompt}`;
    }
  }

  const parts = input.split(/\s+/);
  const cmd = parts[0];
  const arg = parts.slice(1).join(' ').replace(/['"]/g, '');

  switch (cmd) {
    case 'help':
    case '?':
      return `
\x1b[1mAvailable commands:\x1b[0m
  look            — look around the room
  go <direction>  — move (north, south, east, west, up, down)
  read <item>     — examine an item in the room
  take <item>     — pick up an item
  inventory       — check what you're carrying
  use <item>      — use an item from your inventory
  help            — show this message
  quit            — disconnect

`;

    case 'look':
    case 'l':
      return look(session);

    case 'go':
    case 'move':
    case 'walk':
    case 'north': case 'south': case 'east': case 'west': case 'up': case 'down':
    case 'n': case 's': case 'e': case 'w': case 'u': case 'd': {
      const dirMap = { n: 'north', s: 'south', e: 'east', w: 'west', u: 'up', d: 'down' };
      let dir = arg || dirMap[cmd] || cmd;
      if (dirMap[dir]) dir = dirMap[dir];

      const room = session.rooms[session.room];

      // Check locked exits
      if (room.locked && room.locked[dir]) {
        if (room.lockItem && session.inventory.includes(room.lockItem)) {
          // Unlock it
          room.exits[dir] = room.locked[dir];
          delete room.locked[dir];
          const unlockText = room.unlockMsg || 'You unlock the way forward.';
          // If there's also a puzzle, enter puzzle mode
          if (room.puzzle && !room.puzzleSolved) {
            session.inputMode = 'puzzle';
            return `\n${unlockText}\n\nThe keypad awaits your input.\n${room.puzzlePrompt}`;
          }
          session.room = room.exits[dir];
          log(session.ip, `MOVED to ${session.room}`);
          return `\n${unlockText}\n` + look(session);
        }
        return '\nThe way is locked. You need something to get through.\n\n';
      }

      // Check for puzzle gate
      if (room.puzzle && !room.puzzleSolved && room.locked && Object.values(room.locked).length > 0) {
        // Already handled above
      }

      if (room.exits && room.exits[dir]) {
        const dest = room.exits[dir];
        // Check if destination has a puzzle gate
        const destRoom = session.rooms[dest];
        if (destRoom && destRoom.puzzle && !destRoom.puzzleSolved && destRoom.locked) {
          // Let them enter the room
        }
        session.room = dest;
        log(session.ip, `MOVED to ${session.room}`);
        return look(session);
      }

      return '\nYou can\'t go that way.\n\n';
    }

    case 'read':
    case 'examine':
    case 'cat':
    case 'open':
    case 'check':
    case 'inspect': {
      const room = session.rooms[session.room];
      const itemKey = Object.keys(room.items || {}).find(k =>
        k === arg || arg.includes(k.replace(/[._]/g, ' ')) || k.includes(arg.replace(/\s+/g, '_'))
      );
      if (itemKey) {
        log(session.ip, `READ ${itemKey}`);
        return `\n\x1b[0;37m${room.items[itemKey]}\x1b[0m\n\n`;
      }
      // Check inventory
      if (session.inventory.includes(arg)) {
        return `\nYou look at the ${arg}. Nothing special to read on it.\n\n`;
      }
      return '\nYou don\'t see that here.\n\n';
    }

    case 'take':
    case 'grab':
    case 'get':
    case 'pickup': {
      const room = session.rooms[session.room];
      if (room.pickup) {
        const item = room.pickup;
        if (arg.includes(item) || item.includes(arg)) {
          if (session.inventory.includes(item)) {
            return `\nYou already have the ${item}.\n\n`;
          }
          session.inventory.push(item);
          delete room.pickup;
          log(session.ip, `TOOK ${item}`);
          return `\nYou pick up the \x1b[1;33m${item}\x1b[0m.\n\n`;
        }
      }
      return '\nYou can\'t take that.\n\n';
    }

    case 'inventory':
    case 'inv':
    case 'i':
      if (session.inventory.length === 0) {
        return '\nYou\'re not carrying anything.\n\n';
      }
      return `\n\x1b[1mInventory:\x1b[0m ${session.inventory.join(', ')}\n\n`;

    case 'use': {
      const room = session.rooms[session.room];
      if (session.inventory.includes(arg)) {
        if (room.lockItem === arg && room.locked) {
          const dir = Object.keys(room.locked)[0];
          room.exits[dir] = room.locked[dir];
          delete room.locked[dir];
          const unlockText = room.unlockMsg || 'You use the ' + arg + '. The way opens.';
          if (room.puzzle && !room.puzzleSolved) {
            session.inputMode = 'puzzle';
            return `\n${unlockText}\n\nThe keypad awaits your input.\n${room.puzzlePrompt}`;
          }
          return `\n\x1b[1;32m${unlockText}\x1b[0m\n\n`;
        }
        return `\nYou can\'t use the ${arg} here.\n\n`;
      }
      return '\nYou don\'t have that.\n\n';
    }

    case 'quit':
    case 'exit':
    case 'logout':
    case 'q':
      return 'QUIT';

    // Fun easter eggs for common scanner/hacker commands
    case 'ls':
      return '\n. .. .bash_history .ssh/ not_the_droids_youre_looking_for/\n\n';
    case 'pwd':
      return '\n/honeypot/maze/you_are_here\n\n';
    case 'whoami':
      return '\na]visitor (uid=1337)\n\n';
    case 'id':
      return '\nuid=1337(visitor) gid=1337(guests) groups=1337(guests),0(definitely_not_root)\n\n';
    case 'uname':
      return '\nMudPotOS 4.2.0-honeypot #1 SMP x86_64 GNU/Linux\n\n';
    case 'sudo':
      return '\n[sudo] password for visitor: Nice try.\n\n';
    case 'wget':
    case 'curl':
      return '\nThe network here only goes deeper, not out.\n\n';
    case 'rm':
      return '\nYou hear a distant laugh. Nothing was deleted.\n\n';
    case 'ssh':
      return '\nYou\'re already inside. How much deeper do you want to go?\n\n';

    default:
      return `\nI don\'t understand "${cmd}". Type \x1b[1mhelp\x1b[0m for commands.\n\n`;
  }
}

// ── Server ─────────────────────────────────────────────────────────────

const BANNER = `
\x1b[1;32m╔══════════════════════════════════════════════════════════════╗
║                                                              ║
║   ███╗   ███╗██╗   ██╗██████╗ ██████╗  ██████╗ ████████╗    ║
║   ████╗ ████║██║   ██║██╔══██╗██╔══██╗██╔═══██╗╚══██╔══╝    ║
║   ██╔████╔██║██║   ██║██║  ██║██████╔╝██║   ██║   ██║       ║
║   ██║╚██╔╝██║██║   ██║██║  ██║██╔═══╝ ██║   ██║   ██║       ║
║   ██║ ╚═╝ ██║╚██████╔╝██████╔╝██║     ╚██████╔╝   ██║       ║
║   ╚═╝     ╚═╝ ╚═════╝ ╚═════╝ ╚═╝      ╚═════╝    ╚═╝       ║
║                                                              ║
║         Welcome to the server. You look tired.               ║
║         Why not stay a while and explore?                     ║
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
      var delay = 500 + Math.floor(Math.random() * 1000);
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
    connectionCount--;
    log(ip, `ERROR: ${err.message}`);
  });
});

server.listen(PORT, () => {
  console.log(`MudPot listening on port ${PORT}`);
  log('SYSTEM', `Server started on port ${PORT}`);
});
