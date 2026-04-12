const log = require('./log');
const rooms = require('./rooms');

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

module.exports = { createSession, look, handleInput };
