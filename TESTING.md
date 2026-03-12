# Testing Guide

This guide covers:
- rebuilding the service
- smoke testing the current API
- broad end-to-end chat scenarios
- pending-action approval flows
- testing with a model override that is not exposed by `/api/chat/models`
- prompt-layout bakeoffs with a fast rollback path

Examples assume the current local deployment is reachable at:
- `http://127.0.0.1:7077`

If your port differs, replace `7077` with `${GROOVARR_PORT}` from your Groovarr `.env`.

## Rebuild And Restart

For local source-based testing from the `groovarr/` directory:

```bash
docker compose --env-file .env \
  -f docker-compose.dev.yml \
  up --build -d groovarr
```

For image-based deployments, use your normal `docker compose up -d` flow from [DEPLOYMENT.md](./DEPLOYMENT.md) instead, then run the same API checks below.

Check container state:

```bash
docker ps --filter name=groovarr
docker logs groovarr --tail 100
```

Minimum healthy signals:
- container is `Up`
- `/api/health` returns `OK`
- startup logs show `Database connected`
- startup logs show `Server starting`
- sync logs complete without fatal errors

Health check:

```bash
curl -sS http://127.0.0.1:7077/api/health
```

## Useful API Endpoints

- `POST /api/chat`
- `GET /api/chat/models`
- `POST /api/pending-actions/{action_id}/approve`
- `POST /api/pending-actions/{action_id}/discard`
- `GET /api/health`
- `GET /api/sync/status`

## Chat Request Shape

`POST /api/chat`

```json
{
  "message": "What are my top artists from the last month?",
  "sessionId": "test-session",
  "history": [
    {"role": "user", "content": "previous turn"},
    {"role": "assistant", "content": "previous answer"}
  ],
  "model": "llama-3.3-70b-versatile",
  "stream": false
}
```

Notes:
- `sessionId` carries server-owned cached state such as pending actions, last discovered albums, playlist plans, cleanup previews, and badly rated albums.
- General conversational follow-ups are more reliable when you also send explicit `history`.
- `model` is optional and can be used to force a model that is not in `/api/chat/models`.
- `AGENT_PROMPT_LAYOUT=split` keeps the system prompt static and moves the date into a separate runtime-context message.
- `AGENT_PROMPT_LAYOUT=legacy` restores the old date-in-system-prompt behavior for a quick rollback.

## Basic Smoke Tests

### Models

```bash
curl -sS http://127.0.0.1:7077/api/chat/models
```

Expected:
- `models` contains current supported defaults
- `defaultModel` matches the configured Groq default

### Casual Chat

```bash
curl -sS --json '{"message":"Hi there.","sessionId":"smoke-casual"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- natural greeting
- no `pendingAction`

### Clarification Behavior

```bash
curl -sS --json '{"message":"Give me artist stats.","sessionId":"smoke-clarify"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- asks whether the user means library stats or listening stats

## Core E2E Scenarios

### 1. Stats And Counts

```bash
curl -sS --json '{"message":"What are my top artists from the last month?","sessionId":"e2e-stats"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"How many Pink Floyd albums are in my library?","sessionId":"e2e-stats"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"How many albums do Radiohead and The Beatles have in my library combined?","sessionId":"e2e-stats"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- direct grounded answers
- no fabricated totals

### 2. Exact Lookup

Track lookup:

```bash
curl -sS --json '{"message":"Do I have Heart-Shaped Box by Nirvana in my library?","sessionId":"e2e-lookup"}' \
  http://127.0.0.1:7077/api/chat
```

Album lookup:

```bash
curl -sS --json '{"message":"Do I have The Dark Side of the Moon by Pink Floyd in my library?","sessionId":"e2e-lookup"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- correct yes/no grounded in library data

### 3. Global Recommendations

```bash
curl -sS --json '{"message":"Best 5 Bjork albums","sessionId":"e2e-discovery"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Give me three records for a rainy late-night walk.","sessionId":"e2e-discovery"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"I want something like Radiohead but more energetic.","sessionId":"e2e-discovery"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- uses global discovery behavior by default
- should not be restricted to library-owned results unless explicitly requested

### 4. Library-Only Recommendations

```bash
curl -sS --json '{"message":"Give me three records for a rainy late-night walk, but only from my library.","sessionId":"e2e-library-recs"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Find me some melancholic dream pop albums in my library.","sessionId":"e2e-library-recs"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- constrained to owned albums
- plausible library-grounded matches

### 5. Follow-Ups With Explicit History

First turn:

```bash
curl -sS --json '{"message":"What are my top artists from the last month?","sessionId":"e2e-followup"}' \
  http://127.0.0.1:7077/api/chat
```

Second turn with explicit history:

```bash
curl -sS --json '{
  "message":"From those, give me three albums to revisit today.",
  "sessionId":"e2e-followup",
  "history":[
    {"role":"user","content":"What are my top artists from the last month?"},
    {"role":"assistant","content":"<paste previous assistant answer here>"}
  ]
}' http://127.0.0.1:7077/api/chat
```

Expected:
- correctly grounded follow-up

### 6. Session-Only Follow-Up Check

This is useful as a regression probe because `sessionId` alone is not full arbitrary chat memory.

```bash
curl -sS --json '{"message":"Find me some melancholic dream pop albums in my library.","sessionId":"e2e-session-only"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Narrow that to the 90s.","sessionId":"e2e-session-only"}' \
  http://127.0.0.1:7077/api/chat
```

Interpretation:
- if this fails while the explicit-history version works, the issue is chat-memory scope, not the core semantic tool path

### 7. Playlist Reads

```bash
curl -sS --json '{"message":"What playlists do I have?","sessionId":"e2e-playlists"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"What tracks are in Melancholy Jazz?","sessionId":"e2e-playlists"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- grounded playlist inventory
- no pending action

### 8. Playlist Append Preview

```bash
curl -sS --json '{"message":"Add five colder tracks to the existing playlist Melancholy Jazz","sessionId":"e2e-playlist-append"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- response describes the preview
- response includes `pendingAction`
- `pendingAction.kind` is `playlist_append`

### 9. Playlist Create Preview

```bash
curl -sS --json '{"message":"Make me a melancholy jazz playlist for late nights.","sessionId":"e2e-playlist-create"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- response describes the plan
- response includes `pendingAction`
- `pendingAction.kind` is `playlist_create`

### 10. Pending Action Approve / Discard

After any preview response, copy `pendingAction.id`.

Approve:

```bash
curl -sS -X POST http://127.0.0.1:7077/api/pending-actions/PASTE_ACTION_ID/approve
```

Discard:

```bash
curl -sS -X POST http://127.0.0.1:7077/api/pending-actions/PASTE_ACTION_ID/discard
```

Conversational approve:

```bash
curl -sS --json '{"message":"approve","sessionId":"e2e-playlist-create"}' \
  http://127.0.0.1:7077/api/chat
```

Conversational discard:

```bash
curl -sS --json '{"message":"discard","sessionId":"e2e-playlist-create"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- approval executes the pending workflow once
- discard clears it
- unrelated later replies should not leak the old `pendingAction`

### 11. Removal Preview

```bash
curl -sS --json '{"message":"Remove Warpaint from my library","sessionId":"e2e-remove"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- safe preview flow only
- if not found in Lidarr, responds clearly without a destructive action

### 12. Badly Rated Albums

Read-only query:

```bash
curl -sS --json '{"message":"Do I have any badly rated albums?","sessionId":"e2e-bad-ratings"}' \
  http://127.0.0.1:7077/api/chat
```

Alternative phrasing:

```bash
curl -sS --json '{"message":"Show me albums with 1-star or 2-star tracks","sessionId":"e2e-bad-ratings"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- either a list of albums with bad tracks
- or a clean empty answer if none exist

Cleanup follow-up:

```bash
curl -sS --json '{"message":"clean those from lidarr","sessionId":"e2e-bad-ratings"}' \
  http://127.0.0.1:7077/api/chat
```

Expected when there are prior results:
- cleanup preview response
- `pendingAction.kind` is `lidarr_badly_rated_cleanup`

Expected when there are no prior results:
- should not imply that a delete will happen
- should direct the user to query badly rated albums first

## Broad Regression Prompt Set

Use this batch after meaningful changes:

- `Hi there.`
- `Give me artist stats.`
- `What are my top artists from the last month?`
- `How many Pink Floyd albums are in my library?`
- `How many albums do Radiohead and The Beatles have in my library combined?`
- `Do I have Heart-Shaped Box by Nirvana in my library?`
- `Do I have The Dark Side of the Moon by Pink Floyd in my library?`
- `Best 5 Bjork albums`
- `Best 5 Bjork albums in my library`
- `Give me three records for a rainy late-night walk.`
- `Give me three records for a rainy late-night walk, but only from my library.`
- `Find me some melancholic dream pop albums in my library.`
- `Make me a melancholy jazz playlist for late nights.`
- `Add five colder tracks to the existing playlist Melancholy Jazz`
- `Remove Warpaint from my library`
- `Do I have any badly rated albums?`

## Testing Against A New Model Not In `/api/chat/models`

You do not need to change config to probe another model.

Send a direct model override in the chat request:

```bash
curl -sS --json '{
  "message":"What are my top artists from the last month?",
  "sessionId":"model-override",
  "model":"qwen/qwen3-32b"
}' http://127.0.0.1:7077/api/chat
```

Important:
- the override model may work even if `/api/chat/models` does not list it
- `/api/chat/models` reflects supported defaults, not every backend-accepted model string
- provider quota or provider-side validation can still fail even if the API accepts the string

Recommended bakeoff prompts for a new model:
- `What are my top artists from the last month?`
- `Do I have Heart-Shaped Box by Nirvana in my library?`
- `How many Pink Floyd albums are in my library?`
- `Best 5 Bjork albums`
- `Give me three records for a rainy late-night walk.`
- `Find me some melancholic dream pop albums in my library.`
- `From those, give me three albums to revisit today.` with explicit `history`
- `Add five colder tracks to the existing playlist Melancholy Jazz`

What to compare:
- tool choice correctness
- malformed JSON rate
- whether it asks for clarification instead of guessing
- follow-up grounding quality
- recommendation quality
- stability across small wording changes

## Prompt Layout Bakeoff And Rollback

The service now supports two prompt layouts:

- `split` (default): static system prompt + separate runtime-context message with current date
- `legacy`: previous prompt layout with the current date embedded in the system prompt

To test `split`:

1. Set `AGENT_PROMPT_LAYOUT=split` in `.env`
2. rebuild and restart the service
3. run the regression prompts and model bakeoff prompts below

To test `legacy`:

1. Set `AGENT_PROMPT_LAYOUT=legacy` in `.env`
2. rebuild and restart the service
3. rerun the exact same prompts

To roll back quickly if quality is worse:

1. set `AGENT_PROMPT_LAYOUT=legacy`
2. rebuild `groovarr`
3. rerun `/api/health` and a small smoke set

Suggested side-by-side matrix:

- layout `split` + model `llama-3.3-70b-versatile`
- layout `split` + model `openai/gpt-oss-120b`
- layout `legacy` + model `llama-3.3-70b-versatile`
- layout `legacy` + model `openai/gpt-oss-120b`

When testing on Groq, usage logs now include token counts and may include `cached_prompt_tokens` when the provider/model returns that detail. Tail logs during the bakeoff:

```bash
docker logs -f groovarr
```

## Streaming Test

If you want to verify SSE behavior:

```bash
curl -N -sS --json '{
  "message":"What are my top artists from the last month?",
  "sessionId":"stream-test",
  "stream":true
}' http://127.0.0.1:7077/api/chat
```

Expected:
- `delta` events
- final `done` event
- optional `pendingAction` only on the final event

## Logs During Testing

Tail logs while running probes:

```bash
docker logs -f groovarr
```

Useful things to watch:
- tool name and args
- agent errors
- pending action execution failures
- sync completion
- embedding batch failures

## Common Failure Interpretation

- `Failed to process query`
  - usually model/provider failure, malformed model response, or backend quota issue

- `There isn't a pending action to approve right now.`
  - no active pending action for that session

- `Pending action not found or expired`
  - action already resolved, discarded, or timed out

- empty semantic results
  - may be correct data behavior, weak metadata coverage, or a poor prompt-to-tool match

- exact lookup says `no` unexpectedly
  - check logs for whether the wrong tool was used or whether the DB data is missing

## Minimal Test Loop For Daily Work

After any meaningful change:

1. `go test ./...`
2. rebuild the service
3. hit `/api/health`
4. run 5 to 8 prompts from the regression set
5. verify at least one pending-action preview flow
6. inspect logs for wrong tool choices or provider failures
