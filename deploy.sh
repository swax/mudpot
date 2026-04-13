#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/.env" ]; then
    set -a; source "$SCRIPT_DIR/.env"; set +a
fi

SRC="${MUDPOT_SRC:-$SCRIPT_DIR/src}"
DIST="${MUDPOT_DIST:-$SCRIPT_DIR/dist}"
DEST="${MUDPOT_DEST:?MUDPOT_DEST not set — copy .env.example to .env}"
WWW="${MUDPOT_WWW:?MUDPOT_WWW not set — copy .env.example to .env}"

echo "Building TypeScript..."
npm run build

echo "Deploying *.js to $DEST..."
cp "$DIST"/*.js "$DEST/"
chown mudpot:mudpot "$DEST"/*.js

echo "Deploying *.php to $WWW..."
cp "$SRC"/*.php "$WWW/"
cp "$SRC"/.env "$WWW/"
chown mudpot:mudpot "$WWW"/*.php "$WWW"/.env

echo "Restarting pm2 process..."
sudo -u mudpot bash -c "cd $DEST && pm2 delete mudpot" 2>/dev/null || true
sudo -u mudpot bash -c "cd $DEST && MUDPOT_PORT='$MUDPOT_PORT' pm2 start server.js --name mudpot && pm2 save"

echo "Deploy complete."
