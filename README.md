# MudPot

A honeypot disguised as a text-based MUD (Multi-User Dungeon). It listens on a configurable TCP port and lures network scanners into exploring a fake server environment, wasting their time while logging everything they do.

## How it works

Visitors connect via telnet and find themselves in a multi-room "server" they can explore with text commands (`look`, `go north`, `read`, `take`, etc.). The rooms are themed as server infrastructure — lobbies, archives, a dev lab — with fake credentials and red herrings scattered throughout. Locked doors, a keycard puzzle, and a password challenge gate progress deeper into the maze, culminating in a vault full of bogus secrets.

All sessions are logged to `/var/log/mudpot.log`, including commands entered, rooms visited, and items picked up.

## Running

```
node server.js
```

The server listens on port `2222` by default. Set the `MUDPOT_PORT` environment variable to change it:

```
MUDPOT_PORT=2323 node server.js
```

Connect to it with:

```
telnet localhost 2222
```

## Running with PM2

To keep MudPot running in the background and restart it on crashes or reboots:

```
npm install -g pm2
pm2 start server.js --name mudpot
```

Set the port via environment variable:

```
MUDPOT_PORT=2323 pm2 start server.js --name mudpot
```

To auto-start on boot:

```
pm2 startup
pm2 save
```

## Dashboard

`index.php` is a stats dashboard that parses the log file and shows session history, room traffic, top commands, and how far visitors got through the maze. Serve it with any PHP-capable web server (e.g. `php -S localhost:8080`).

## Requirements

- Node.js
- PHP (for the dashboard only)
