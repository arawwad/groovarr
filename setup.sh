#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
COMPOSE_FILE="${GROOVARR_COMPOSE_FILE:-docker-compose.dev.yml}"
ENV_FILE="${GROOVARR_ENV_FILE:-$SCRIPT_DIR/.env}"

echo "Setting up Groovarr from $SCRIPT_DIR"

if [ ! -f "$ENV_FILE" ]; then
    echo ".env file not found at $ENV_FILE. Copy .env.example and configure it first."
    exit 1
fi

set -a
# shellcheck source=/dev/null
. "$ENV_FILE"
set +a

DATA_DIR="${GROOVARR_DATA_DIR:-$SCRIPT_DIR/groovarr-data}"
mkdir -p "$DATA_DIR/postgres" "$DATA_DIR/queue/weekly-queue" "$DATA_DIR/queue/playlist-processed" "$DATA_DIR/downloads/inbox" "$DATA_DIR/slsk-batchdl" "$DATA_DIR/beets"

cd "$SCRIPT_DIR"

echo "Starting Groovarr using $COMPOSE_FILE"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d --build

echo
echo "Groovarr is starting."
echo
echo "Check status:"
echo "  docker compose --env-file $ENV_FILE -f $COMPOSE_FILE ps"
echo
echo "View logs:"
echo "  docker compose --env-file $ENV_FILE -f $COMPOSE_FILE logs -f groovarr"
echo
echo "Access at:"
echo "  http://127.0.0.1:${GROOVARR_PORT:-7077}"
