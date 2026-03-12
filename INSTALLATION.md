# Installation

This guide assumes you already have these services running somewhere reachable from Groovarr:

- Navidrome
- Lidarr
- one LLM backend via Groq or Hugging Face credentials

Optional for better semantic search quality:

- embeddings endpoint

## 1. Copy the env template

```bash
cp .env.example .env
```

## 2. Fill in the required settings

At minimum, set these values in `.env`:

```bash
GROOVARR_DB_PASSWORD=change_me
GROOVARR_PORT=7077

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
```

Important notes:

- `NAVIDROME_DATA_PATH` must point to the directory that contains `navidrome.db`.
- At least one of `GROQ_API_KEY` or `HUGGINGFACE_API_KEY` must be set.
- `EMBEDDINGS_ENDPOINT` is optional for now. If left blank, vibe/semantic search will be limited.

## 3. Review storage paths

Groovarr keeps its own runtime data under:

- `GROOVARR_DATA_DIR`

The downloader/importer path expects a real music library destination through:

- `BEETS_LIBRARY_PATH`

If you already use a shared library path with Navidrome, point `BEETS_LIBRARY_PATH` at that location.

## 4. Start Groovarr

Image-first deployment:

```bash
docker compose --env-file .env up -d
```

Local build/dev deployment:

```bash
docker compose -f docker-compose.dev.yml --env-file .env up -d --build
```

## 5. Verify the service

Once the containers are up:

```bash
curl -fsS http://localhost:7077/api/health
```

Expected response:

```text
OK
```

## 6. Open the UI

Browse to:

```text
http://localhost:7077
```

## Troubleshooting

- If Groovarr starts but library answers are empty, re-check `NAVIDROME_DATA_PATH`.
- If playlist updates work but vibe-style search is weak, configure `EMBEDDINGS_ENDPOINT`.
- If Lidarr actions fail, verify `LIDARR_URL`, `LIDARR_API_KEY`, and `LIDARR_ROOT_FOLDER_PATH`.
- If download automation does nothing, verify `SLSK_USERNAME` and `SLSK_PASSWORD`.

For full deployment details and the complete env surface, see [DEPLOYMENT.md](./DEPLOYMENT.md).
