#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/.env" ]; then
    set -a; source "$SCRIPT_DIR/.env"; set +a
fi

SRC="${MUDPOT_SRC:-$SCRIPT_DIR}"
DEST="${MUDPOT_DEST:?MUDPOT_DEST not set — copy .env.example to .env}"
WWW="${MUDPOT_WWW:?MUDPOT_WWW not set — copy .env.example to .env}"

echo "Deploying *.js to $DEST..."
cp "$SRC"/*.js "$DEST/"
chown mudpot:mudpot "$DEST"/*.js

echo "Deploying *.php to $WWW..."
cp "$SRC"/*.php "$WWW/"
cp "$SRC"/.env "$WWW/"
chown mudpot:mudpot "$WWW"/*.php "$WWW"/.env

echo "Restarting pm2 process..."
sudo -u mudpot pm2 restart mudpot

echo "Deploy complete."
