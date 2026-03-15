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

SONIC_ANALYSIS_ENABLED="${SONIC_ANALYSIS_ENABLED:-true}"

overlay_file() {
    case "$1" in
        docker-compose.dev.yml) printf '%s' "docker-compose.dev.sonic.yml" ;;
        docker-compose.yml) printf '%s' "docker-compose.sonic.yml" ;;
        *) printf '%s' "" ;;
    esac
}

SONIC_COMPOSE_FILE="$(overlay_file "$COMPOSE_FILE")"
set -- docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE"
if [ "$SONIC_ANALYSIS_ENABLED" != "false" ] && [ "$SONIC_ANALYSIS_ENABLED" != "0" ] && [ "$SONIC_ANALYSIS_ENABLED" != "no" ] && [ "$SONIC_ANALYSIS_ENABLED" != "off" ] && [ -n "$SONIC_COMPOSE_FILE" ]; then
    set -- "$@" -f "$SONIC_COMPOSE_FILE"
fi

DATA_DIR="${GROOVARR_DATA_DIR:-$SCRIPT_DIR/groovarr-data}"
mkdir -p "$DATA_DIR/postgres" "$DATA_DIR/queue/weekly-queue" "$DATA_DIR/queue/playlist-processed" "$DATA_DIR/downloads/inbox" "$DATA_DIR/slsk-batchdl" "$DATA_DIR/beets"

cd "$SCRIPT_DIR"

echo "Starting Groovarr using $COMPOSE_FILE"
"$@" up -d --build

echo
echo "Groovarr is starting."
echo
echo "Check status:"
printf '  %s ps\n' "$*"
echo
echo "View logs:"
printf '  %s logs -f groovarr\n' "$*"
echo
echo "Access at:"
echo "  http://127.0.0.1:${GROOVARR_PORT:-7077}"
