import log from "./log";
import rooms from "./rooms";
import { Room, Session } from "./types";

function tryUnlock(session: Session, room: Room, dir: string): string {
  if (room.puzzle && !room.puzzleSolved) {
    session.inputMode = "puzzle";
    return "puzzle";
  }
  room.exits[dir] = room.locked![dir];
  delete room.locked![dir];
  return "unlocked";
}

export function createSession(ip: string): Session {
  return {
    room: "lobby",
    inventory: [],
    ip,
    inputMode: "normal",
    rooms: JSON.parse(JSON.stringify(rooms)),
  };
}

export function look(session: Session): string {
  const room = session.rooms[session.room];
  let text = `\n\x1b[1;33m${room.name}\x1b[0m\n${room.desc}\n`;

  const exits = Object.keys(room.exits || {});
  const locked = Object.keys(room.locked || {});
  const allExits = exits.concat(locked);
  if (allExits.length > 0) {
    text += `\n\x1b[1;36mExits:\x1b[0m ${allExits.join(", ")}`;
  }
  text += "\n";
  return text;
}

export function handleInput(session: Session, raw: string): string {
  const input = raw.trim().toLowerCase();
  if (!input) return "";

  log(session.ip, `[${session.room}] ${raw.trim()}`);

  // Puzzle input mode
  if (session.inputMode === "puzzle") {
    const room = session.rooms[session.room];
    if (input.replace(/\s+/g, "") === room.puzzleAnswer) {
      room.puzzleSolved = true;
      // Unlock the door
      const dir = Object.keys(room.locked!)[0];
      room.exits[dir] = room.locked![dir];
      delete room.locked![dir];
      session.inputMode = "normal";
      return `\n\x1b[1;32m${room.puzzleSuccess}\x1b[0m\n\n`;
    } else if (input === "quit" || input === "back" || input === "cancel") {
      session.inputMode = "normal";
      return "\nYou step back from the panel.\n\n";
    } else {
      return `\n\x1b[1;31m${room.puzzleFail}\x1b[0m\n${room.puzzlePrompt}`;
    }
  }

  const parts = input.split(/\s+/);
  const cmd = parts[0];
  const arg = parts.slice(1).join(" ").replace(/['"]/g, "");

  switch (cmd) {
    case "help":
    case "?":
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

    case "look":
    case "l":
      return look(session);

    case "go":
    case "move":
    case "walk":
    case "north":
    case "south":
    case "east":
    case "west":
    case "up":
    case "down":
    case "n":
    case "s":
    case "e":
    case "w":
    case "u":
    case "d": {
      const dirMap: Record<string, string> = {
        n: "north",
        s: "south",
        e: "east",
        w: "west",
        u: "up",
        d: "down",
      };
      let dir = arg || dirMap[cmd] || cmd;
      if (dirMap[dir]) dir = dirMap[dir];

      const room = session.rooms[session.room];

      // Check locked exits
      if (room.locked && room.locked[dir]) {
        if (room.lockItem && session.inventory.includes(room.lockItem)) {
          const unlockText = room.unlockMsg || "You unlock the way forward.";
          const dest = room.locked[dir];
          const result = tryUnlock(session, room, dir);
          if (result === "puzzle") {
            return `\n${unlockText}\n\nThe panel awaits your input.\n${room.puzzlePrompt}`;
          }
          session.room = dest;
          log(session.ip, `MOVED to ${session.room}`);
          return `\n${unlockText}\n` + look(session);
        }
        return "\nThe way is locked. You need something to get through.\n\n";
      }

      if (room.exits && room.exits[dir]) {
        const dest = room.exits[dir];
        // Check if destination has a puzzle gate
        const destRoom = session.rooms[dest];
        if (
          destRoom &&
          destRoom.puzzle &&
          !destRoom.puzzleSolved &&
          destRoom.locked
        ) {
          // Let them enter the room
        }
        session.room = dest;
        log(session.ip, `MOVED to ${session.room}`);
        return look(session);
      }

      return "\nYou can't go that way.\n\n";
    }

    case "read":
    case "examine":
    case "cat":
    case "open":
    case "check":
    case "inspect": {
      const room = session.rooms[session.room];
      const itemKey = Object.keys(room.items || {}).find(
        (k) =>
          k === arg ||
          arg.includes(k.replace(/[._]/g, " ")) ||
          k.includes(arg.replace(/\s+/g, "_")),
      );
      if (itemKey) {
        log(session.ip, `READ ${itemKey}`);
        if (session.room === "vault" && itemKey === "data_crystal") {
          log(session.ip, "VICTORY");
          return (
            "VICTORY\n\x1b[0;37m" +
            room.items[itemKey] +
            "\x1b[0m\n" +
            "\n\x1b[1;36m══════════════════════════════════════════════\x1b[0m\n" +
            "\n\x1b[1;37mYou have reached the heart of Grey Sector.\x1b[0m\n" +
            "\x1b[0;36mFew have made it this far. Whatever truth was\n" +
            "hidden here, you now carry it with you.\x1b[0m\n" +
            "\n\x1b[1;36m══════════════════════════════════════════════\x1b[0m\n\n"
          );
        }
        return `\n\x1b[0;37m${room.items[itemKey]}\x1b[0m\n\n`;
      }
      // Check inventory
      if (session.inventory.includes(arg)) {
        return `\nYou look at the ${arg}. Nothing special to read on it.\n\n`;
      }
      return "\nYou don't see that here.\n\n";
    }

    case "take":
    case "grab":
    case "get":
    case "pickup": {
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
      return "\nYou can't take that.\n\n";
    }

    case "inventory":
    case "inv":
    case "i":
      if (session.inventory.length === 0) {
        return "\nYou're not carrying anything.\n\n";
      }
      return `\n\x1b[1mInventory:\x1b[0m ${session.inventory.join(", ")}\n\n`;

    case "use": {
      const room = session.rooms[session.room];
      if (session.inventory.includes(arg)) {
        if (
          room.lockItem === arg &&
          room.locked &&
          Object.keys(room.locked).length > 0
        ) {
          const dir = Object.keys(room.locked)[0];
          const unlockText =
            room.unlockMsg || "You use the " + arg + ". The way opens.";
          const result = tryUnlock(session, room, dir);
          if (result === "puzzle") {
            return `\n${unlockText}\n\nThe panel awaits your input.\n${room.puzzlePrompt}`;
          }
          return `\n\x1b[1;32m${unlockText}\x1b[0m\n\n`;
        }
        return `\nYou can't use the ${arg} here.\n\n`;
      }
      return "\nYou don't have that.\n\n";
    }

    case "quit":
    case "exit":
    case "logout":
    case "q":
      return "QUIT";

    case "ls":
      return "\nThe corridor stretches in both directions. Try 'look' to survey your surroundings.\n\n";
    case "pwd":
      return "\nSomewhere in Grey Sector. The station directory on the wall is too faded to read.\n\n";
    case "whoami":
      return "\nA wanderer. You came down here for a reason, even if you've forgotten what it was.\n\n";
    case "id":
      return "\nYour station ident reads: VISITOR — UNRESTRICTED TRANSIT — LEVEL GREY\n\n";
    case "uname":
      return "\nBabylon Station — Grey Sector — Lower Levels\n\n";
    case "sudo":
      return "\nThat authority doesn't reach this deep.\n\n";
    case "wget":
    case "curl":
      return "\nThere's no comm signal this far down.\n\n";
    case "rm":
      return "\nSome things in Grey Sector can't be removed. They've been here longer than the station.\n\n";
    case "ssh":
      return "\nYou're already deeper than most people go.\n\n";

    // Common bot credentials — the station reacts to intruders
    case "root":
      return "\nThe roots of this station go deeper than you know. You are not one of them.\n\n";
    case "admin":
      return "\nStation administration is three levels up in Blue Sector. You're in the wrong corridor.\n\n";
    case "super":
      return "\nThere are no superiors down here. Grey Sector answers to no one.\n\n";
    case "ubnt":
      return "\nThat frequency hasn't been used since the comm relays were decommissioned.\n\n";
    case "keomeo":
      return "\nA word in a dialect the station translator doesn't recognize. The walls hum faintly.\n\n";
    case "!!huawei":
      return "\nEmergency override rejected. This section operates outside normal command authority.\n\n";
    case "user":
      return "\nThe station knows what you are. The question is whether you know where you are.\n\n";
    case "telnet":
      return "\nYou're already speaking to the station. It has been listening since you arrived.\n\n";
    case "e8telnet":
      return "\nAn old comm protocol echoes through the corridor and fades. The station does not respond.\n\n";
    case "e8ehome1":
      return "\nHome is very far from here. No one comes to Grey Sector by accident.\n\n";
    case "guest":
      return "\nGuest quarters are on the upper levels. Down here, everyone is either lost or looking.\n\n";

    default:
      return `\nI don't understand "${cmd}". Type \x1b[1mhelp\x1b[0m for commands.\n\n`;
  }
}
