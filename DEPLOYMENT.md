# Groovarr Deployment Guide

## Scope

This guide describes the primary deployment model for Groovarr.

Bundled services:

- `groovarr-db`
- `groovarr`
- `groovarr-downloader` (`slsk-batchdl`)
- `groovarr-importer` (`beets`)

External systems expected to already exist:

- Navidrome
- Lidarr
- one LLM backend reachable through Groq or Hugging Face credentials

Not bundled in this phase:

- embeddings
- Ollama
- `slskd`

## Config

Start from [.env.example](./.env.example).

Required:

```bash
GROOVARR_DB_PASSWORD=change_me
GROOVARR_PORT=7077
GROOVARR_DATA_DIR=./groovarr-data
GROOVARR_INTEGRATION_NETWORK=docker_media-network
BEETS_LIBRARY_PATH=/absolute/path/to/media/music-beets/library
GROOVARR_APP_IMAGE=ghcr.io/your-org/groovarr:latest
GROOVARR_DB_IMAGE=ghcr.io/your-org/groovarr-db:latest
GROOVARR_DOWNLOADER_IMAGE=ghcr.io/your-org/groovarr-downloader:latest
GROOVARR_IMPORTER_IMAGE=ghcr.io/your-org/groovarr-importer:latest

NAVIDROME_URL=http://navidrome:4533
NAVIDROME_USERNAME=your_navidrome_username
NAVIDROME_PASSWORD=your_navidrome_password
NAVIDROME_DATA_PATH=/absolute/path/to/navidrome/data

LIDARR_URL=http://lidarr:8686
LIDARR_API_KEY=your_lidarr_api_key

SLSK_USERNAME=your_soulseek_username
SLSK_PASSWORD=your_soulseek_password
```

One model backend is also required:

```bash
GROQ_API_KEY=
HUGGINGFACE_API_KEY=
DEFAULT_CHAT_MODEL=llama-3.3-70b-versatile
```

Optional:

```bash
EMBEDDINGS_ENDPOINT=
SYNC_LASTFM_ENABLED=false
LASTFM_API_KEY=
SYNC_LASTFM_ALBUMS_PER_SYNC=10
```

Last.fm note:

- Groovarr can enrich album metadata directly from Last.fm during sync
- `LASTFM_API_KEY` is required only when `SYNC_LASTFM_ENABLED=true`

## Start

From this directory:

```bash
cp .env.example .env
docker compose --env-file .env up -d
```

Postgres is intentionally internal to this bundle:

- no Postgres port is published to the host
- data lives on the host under `${GROOVARR_DATA_DIR}/postgres`
- only Groovarr-side containers on the compose network can reach `groovarr-db`

Integration with an existing Navidrome/Lidarr stack is done through the external Docker network named by `GROOVARR_INTEGRATION_NETWORK`.

Media import split:

- downloader inbox stays internal under `${GROOVARR_DATA_DIR}/downloads/inbox`
- beets library output goes to `BEETS_LIBRARY_PATH`

For local source-based iteration, use [docker-compose.dev.yml](./docker-compose.dev.yml).

Current downloader packaging note:

- the distribution file expects `GROOVARR_DOWNLOADER_IMAGE`
- the local dev downloader build in [docker-compose.dev.yml](./docker-compose.dev.yml) builds `sldl` from the upstream `fiso64/slsk-batchdl` repository during image build
- downloader/importer services no longer advertise `PUID` / `PGID` in the public env contract because the current wrappers do not implement that mapping

## Endpoints

- UI: `http://localhost:${GROOVARR_PORT}`
- Health: `http://localhost:${GROOVARR_PORT}/api/health`
- Chat API: `http://localhost:${GROOVARR_PORT}/api/chat`

## Verification

Health:

```bash
curl -sS "http://127.0.0.1:${GROOVARR_PORT:-7077}/api/health"
```

Basic chat:

```bash
curl -sS -X POST "http://127.0.0.1:${GROOVARR_PORT:-7077}/api/chat" \
  -H 'Content-Type: application/json' \
  -d '{"message":"what songs I listened to last week"}'
```

Container state:

```bash
docker compose --env-file .env ps
```

Postgres data directory:

```bash
ls -la "${GROOVARR_DATA_DIR:-./groovarr-data}/postgres"
```

## Migrating Existing Data

If you are moving from an older Groovarr deployment, do not copy files into a running database container. Use dump/restore.

Dump from the current stack:

```bash
docker exec groovarr-db pg_dump -U groovarr -d groovarr > groovarr.sql
```

Stop the old Groovarr database so the new stack can reuse the `groovarr-db` container name cleanly, then start the new database so it initializes the host-mounted data directory:

```bash
docker compose --env-file .env up -d groovarr-db
```

Restore into the new internal database:

```bash
docker compose --env-file .env exec -T groovarr-db psql -U groovarr -d groovarr < groovarr.sql
```

Then start the rest of the bundle:

```bash
docker compose --env-file .env up -d
```

## Current Limitation

Current Groovarr still needs `NAVIDROME_DB_PATH` to run the library sync daemon.

- If `EMBEDDINGS_ENDPOINT` is blank, sync still runs but embedding refresh is skipped.
- That keeps library sync working while reducing semantic search quality.

That limitation is known and intentionally deferred.
