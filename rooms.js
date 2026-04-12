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

module.exports = rooms;
