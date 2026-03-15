# Groovarr

Groovarr is a conversational music-operations service that sits next to an existing Navidrome and Lidarr setup.

![Groovarr UI](<./Screenshot 2026-03-12 at 6.04.46 PM.png>)

It provides:

- natural-language chat over your music library
- grounded library stats and listening summaries
- playlist planning, preview, append, and approval flows
- optional Lidarr-oriented discovery workflows
- downloader/import orchestration through bundled sidecars

## Deployment Model

The primary deployment path is the image-first bundle in this directory:

- [docker-compose.yml](./docker-compose.yml)
- [.env.example](./.env.example)
- [INSTALLATION.md](./INSTALLATION.md)
- [DEPLOYMENT.md](./DEPLOYMENT.md)
- [DEPENDENCIES.md](./DEPENDENCIES.md)

Supporting local-build/dev assets:

- [docker-compose.dev.yml](./docker-compose.dev.yml)
- [Dockerfile](./Dockerfile)
- [Dockerfile.db](./Dockerfile.db)
- [Dockerfile.downloader](./Dockerfile.downloader)
- [Dockerfile.importer](./Dockerfile.importer)

That bundle includes:

- `groovarr`
- `groovarr-db`
- `groovarr-downloader` (`slsk-batchdl`)
- `groovarr-importer` (`beets`)

It assumes these systems already exist outside Groovarr:

- Navidrome
- Lidarr
- one LLM backend reachable through Groq or Hugging Face credentials

Current V1 scope does not package:

- embeddings
- Ollama
- `slskd`

## What Works Now

- Natural-language chat endpoint: `/api/chat`
- Tool-driven agent flow with strict JSON actions (`query` / `respond`)
- PostgreSQL + pgvector storage for artists, albums, tracks, and play events
- Snapshot-style sync from Navidrome SQLite with stale album/track pruning
- Event-based listening analytics (`play_events` from Navidrome `scrobbles`)
- Conversational album discovery and library-grounded recommendation flows
- Playlist planning, preview, append, and approval flows
- Lidarr/discovered-album preview workflows
- Compact web UI for manual chat testing
- Listen and Sonic Studio surfaces for runtime context, Music Map browsing, Song Path bridging, scene browsing, and vibe search

## Important Current Limits

- Sync startup requires `NAVIDROME_DB_PATH`
- Vibe search quality depends on track embeddings being populated
- Leaving `EMBEDDINGS_ENDPOINT` blank skips embedding refresh and weakens semantic/vibe search quality
- If event history is empty for a window, the app falls back to track metadata (`play_count` / `last_played`)

## Config Contract

The main external env contract is:

```bash
GROOVARR_DB_PASSWORD=change_me
GROOVARR_PORT=7077
GROOVARR_DATA_DIR=./groovarr-data

NAVIDROME_URL=http://navidrome:4533
NAVIDROME_USERNAME=your_user
NAVIDROME_PASSWORD=your_pass
NAVIDROME_DATA_PATH=/absolute/path/to/navidrome/data

LIDARR_URL=http://lidarr:8686
LIDARR_API_KEY=your_key

SLSK_USERNAME=your_soulseek_username
SLSK_PASSWORD=your_soulseek_password

GROQ_API_KEY=
HUGGINGFACE_API_KEY=
DEFAULT_CHAT_MODEL=llama-3.3-70b-versatile
EMBEDDINGS_ENDPOINT=
SIMILARITY_DEFAULT_PROVIDER=hybrid
SONIC_ANALYSIS_ENABLED=true
SYNC_LASTFM_ENABLED=false
LASTFM_API_KEY=
SYNC_LASTFM_ALBUMS_PER_SYNC=10
```

Last.fm note:

- Groovarr can enrich album metadata directly from Last.fm during sync
- `LASTFM_API_KEY` is optional but required if `SYNC_LASTFM_ENABLED=true`

See [DEPLOYMENT.md](./DEPLOYMENT.md) for the full deployment contract.

### Similarity API

Groovarr now exposes a thin similarity API intended for a Navidrome plugin or any other local client:

- `GET /api/similarity/health`
- `POST /api/similarity/tracks`
- `POST /api/similarity/songs/by-artist`
- `POST /api/similarity/artists`

Providers:

- `local`: Groovarr's own library embeddings
- `audiomuse`: AudioMuse-backed similarity, when configured
- `hybrid`: merge AudioMuse and local candidates, then rerank in Groovarr

Track request example:

```json
{
  "seedTrackId": "navidrome-track-id",
  "provider": "hybrid",
  "limit": 25,
  "excludeRecentDays": 14,
  "excludeSeedArtist": false
}
```

### Sonic Studio

The web UI now includes a dedicated visual exploration surface at `/explore` with:

- Music Map point-cloud browsing over the internal sonic map
- Song Path bridging between two tracks
- Scene Shelf browsing from internal clustering output
- text-to-sound vibe search

## Installation

For the shortest path, follow [INSTALLATION.md](./INSTALLATION.md).

The high-level flow is:

1. Copy [.env.example](./.env.example) to `.env`.
2. Point Groovarr at your existing Navidrome and Lidarr services.
3. Mount your Navidrome data directory so Groovarr can read `navidrome.db`.
4. Provide Soulseek credentials for the bundled downloader sidecar.
5. Start the stack with `docker compose --env-file .env up -d`.

## Image Model

The default [docker-compose.yml](./docker-compose.yml) is now image-first.

It expects image names through these env vars:

- `GROOVARR_APP_IMAGE`
- `GROOVARR_DB_IMAGE`
- `GROOVARR_DOWNLOADER_IMAGE`
- `GROOVARR_IMPORTER_IMAGE`

For local iteration inside this repo, use [docker-compose.dev.yml](./docker-compose.dev.yml) instead.

Current packaging notes:

- [Dockerfile.downloader](./Dockerfile.downloader) builds `sldl` from the upstream `fiso64/slsk-batchdl` repository during image build
- the public deployment contract expects a published `GROOVARR_DOWNLOADER_IMAGE`
- downloader/importer containers currently do not expose `PUID` / `PGID` knobs in the public env contract

## Development

```bash
cd groovarr
go mod tidy
make test
go run ./cmd/server
```

Notes:

- `make test` is the recommended quality check on this host
- `go test ./...` can fail if local Docker data directories are permission-restricted
- End-to-end API testing instructions live in [TESTING.md](./TESTING.md)

## Architecture

Key code areas:

- `cmd/server/`: HTTP API, chat handlers, workflow state, approvals
- `internal/agent/executor.go`: LLM loop, provider/model selection, JSON action contract
- `internal/toolspec/`: prompt-visible tool catalog
- `internal/db/sync.go`: Navidrome sync, scrobble ingestion, embedding regeneration
- `internal/db/postgres.go`: DB query layer and schema bootstrap
- `graph/`: resolver and schema-facing query logic

For more detail, see [ARCHITECTURE.md](./ARCHITECTURE.md).

For an operator-facing dependency and feature map, see [DEPENDENCIES.md](./DEPENDENCIES.md).
