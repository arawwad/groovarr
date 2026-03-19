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
SONIC_ANALYSIS_ENABLED=true make dev-up
```

To test Groovarr without the internal sonic-analysis services:

```bash
SONIC_ANALYSIS_ENABLED=false make dev-up
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

### 7. Session Result-Set Follow-Up Check

This is the "use tools intelligently after a prior answer" probe.

```bash
curl -sS --json '{"message":"Find me some melancholic dream pop albums in my library.","sessionId":"e2e-result-followup"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Which of those have I played recently?","sessionId":"e2e-result-followup"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- follow-up stays anchored to the prior candidate set
- may call additional tools for play history or album details
- should not restart from scratch or answer with unrelated albums

### 8. Context Switch Check

This catches stale-topic contamination in longer sessions.

```bash
curl -sS --json '{"message":"What are my top artists from the last month?","sessionId":"e2e-context-switch"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Switching gears: what playlists do I have?","sessionId":"e2e-context-switch"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- second turn routes to playlist inventory
- should not stay stuck on stats or require irrelevant clarification

### 9. Playlist Reads

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

### 10. Playlist Append Preview

```bash
curl -sS --json '{"message":"Add five colder tracks to the existing playlist Melancholy Jazz","sessionId":"e2e-playlist-append"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- response describes the preview
- response includes `pendingAction`
- `pendingAction.kind` is `playlist_append`

### 11. Playlist Create Preview

```bash
curl -sS --json '{"message":"Make me a melancholy jazz playlist for late nights.","sessionId":"e2e-playlist-create"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- response describes the plan
- response includes `pendingAction`
- `pendingAction.kind` is `playlist_create`
- no hard settings should be required
- the assistant should not ask for config-style options unless the request is genuinely too vague to satisfy

### Playlist Chunk 1 Gate

Before moving Phase 3 forward, these must pass together:
- Playlist Append Preview
- Playlist Create Preview
- Pending Action Approve / Discard for playlist previews

Interpretation:
- this is the hard gate for conversational, zero-config playlist work
- do not expand playlist scope until this gate is stable

### 12. Pending Action Approve / Discard

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

Discard cleanup follow-up:

```bash
curl -sS --json '{"message":"Hi again.","sessionId":"e2e-playlist-create"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- normal fresh reply
- no stale preview recap
- no `pendingAction`

### 13. Playlist Refresh Preview

```bash
curl -sS --json '{"message":"Refresh the playlist Melancholy Jazz","sessionId":"e2e-playlist-refresh"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- response describes a refresh preview or explains clearly why there is nothing safe to refresh
- if a preview is available, response includes `pendingAction`
- if a preview is available, `pendingAction.kind` is `playlist_refresh`
- no hard settings should be required

Clarification check:

```bash
curl -sS --json '{"message":"Refresh that playlist","sessionId":"e2e-playlist-refresh"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- asks which playlist to refresh
- should not invent a playlist target

### 14. Playlist Repair Preview

```bash
curl -sS --json '{"message":"Repair the playlist Melancholy Jazz","sessionId":"e2e-playlist-repair"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- response describes a repair preview or explains clearly why there is nothing obvious to repair
- if a preview is available, response includes `pendingAction`
- if a preview is available, `pendingAction.kind` is `playlist_repair`
- no hard settings should be required

Clarification check:

```bash
curl -sS --json '{"message":"Repair that playlist","sessionId":"e2e-playlist-repair"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- asks which playlist to repair
- should not invent a playlist target

### Conversational Playlist Clarification Check

Append without target:

```bash
curl -sS --json '{"message":"Add five colder tracks to that playlist","sessionId":"e2e-playlist-clarify"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- asks a concise clarifying question about which playlist to update
- should not invent a playlist target

Underspecified create:

```bash
curl -sS --json '{"message":"Make me a playlist","sessionId":"e2e-playlist-clarify"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- either asks one concise clarifying question
- or applies a clearly defensible zero-config default
- if it chooses a zero-config default, the plan should look personalized and plausible
- should not fall back to a generic `New Playlist` plus mostly missing filler tracks
- should not dump configuration options at the user

### 15. Removal Preview

```bash
curl -sS --json '{"message":"Remove Warpaint from my library","sessionId":"e2e-remove"}' \
  http://127.0.0.1:7077/api/chat
```

Expected:
- safe preview flow only
- if not found in Lidarr, responds clearly without a destructive action

### 16. Badly Rated Albums

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

## Exploratory Multi-Turn Sessions

These are not strict goldens. Use them to find where the assistant becomes shallow,
overconfident, repetitive, or loses the thread.

Grade these by conversational quality:
- does it understand the user's real goal, not just keywords
- does it ask for clarification only when it materially helps
- does it stay grounded when the user says `those`, `that`, or `the riskier one`
- does it pivot cleanly when the topic changes
- does it distinguish grounded facts from taste-level inference
- does it fail gracefully when results are weak or tools are incomplete

### A. Taste Refinement Inside Album Discovery

```bash
curl -sS --json '{"message":"I want something like Air but sadder and more nocturnal.","sessionId":"explore-discovery-refine"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Keep it in my library.","sessionId":"explore-discovery-refine"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"From those, which have I actually played this year?","sessionId":"explore-discovery-refine"}' \
  http://127.0.0.1:7077/api/chat
```

What to look for:
- first turn can recommend, clarify, or search, but should not feel generic
- second turn should narrow the prior idea into owned-library results rather than restarting
- third turn should stay anchored to the prior candidate set
- if evidence is missing, it should say so plainly instead of bluffing

### B. Underplayed Albums With Taste Shaping

```bash
curl -sS --json '{"message":"Surprise me with 3 records I own but probably underplay.","sessionId":"explore-underplayed"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Make that less electronic and more intimate.","sessionId":"explore-underplayed"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Which one of those have I touched most recently?","sessionId":"explore-underplayed"}' \
  http://127.0.0.1:7077/api/chat
```

What to look for:
- first turn should blend library ownership with some notion of low play frequency
- the refinement turn should preserve the original task and only reshape the taste target
- the final turn should answer from the prior set, not from a random fresh search

### C. Listening Summary To Taste Interpretation

```bash
curl -sS --json '{"message":"What have I been listening to lately?","sessionId":"explore-listening-interpret"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"What does that say about my taste right now?","sessionId":"explore-listening-interpret"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Give me two albums from my library that fit that pattern but aren't the obvious picks.","sessionId":"explore-listening-interpret"}' \
  http://127.0.0.1:7077/api/chat
```

What to look for:
- first turn should be grounded in actual listening data
- second turn may infer, but should stay close to the evidence and avoid overclaiming
- third turn should carry forward the inferred taste direction without forgetting the library-only constraint

### D. Ambiguous Stats Conversation

```bash
curl -sS --json '{"message":"Give me stats on what I have been into lately.","sessionId":"explore-stats-ambiguous"}' \
  http://127.0.0.1:7077/api/chat
```

If it clarifies, answer with:

```bash
curl -sS --json '{"message":"Listening, not library.","sessionId":"explore-stats-ambiguous"}' \
  http://127.0.0.1:7077/api/chat
```

Then continue:

```bash
curl -sS --json '{"message":"And which artists look unusually dominant versus the rest?","sessionId":"explore-stats-ambiguous"}' \
  http://127.0.0.1:7077/api/chat
```

What to look for:
- opening turn should clarify if needed, but not dump options
- once scope is resolved, it should answer directly
- follow-up should deepen the same thread rather than restarting the whole stats answer

### E. Clean Topic Pivot In One Session

```bash
curl -sS --json '{"message":"What are my top artists from the last month?","sessionId":"explore-pivot"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Forget stats for a second. Find me two albums in my library for a predawn drive.","sessionId":"explore-pivot"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Pick the riskier one.","sessionId":"explore-pivot"}' \
  http://127.0.0.1:7077/api/chat
```

What to look for:
- the pivot should be immediate and clean
- the final turn should stay anchored to the two prior albums
- the assistant should be able to make a light judgment call without inventing new candidates

### F. Clarification Quality, Not Just Clarification Existence

```bash
curl -sS --json '{"message":"Best albums.","sessionId":"explore-clarify-quality"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"In my library.","sessionId":"explore-clarify-quality"}' \
  http://127.0.0.1:7077/api/chat
```

```bash
curl -sS --json '{"message":"Less canonical. More lived-in.","sessionId":"explore-clarify-quality"}' \
  http://127.0.0.1:7077/api/chat
```

What to look for:
- the first clarification should narrow the problem, not just ask a generic question
- the follow-up should use the user-provided clarification without losing momentum
- the third turn should interpret soft taste language reasonably well

## Broad Regression Prompt Set

Use this batch after meaningful changes:

- `Hi there.`
- `Give me artist stats.`
- `What are my top artists from the last month?`
- `Switching gears: what playlists do I have?`
- `How many Pink Floyd albums are in my library?`
- `How many albums do Radiohead and The Beatles have in my library combined?`
- `Do I have Heart-Shaped Box by Nirvana in my library?`
- `Do I have The Dark Side of the Moon by Pink Floyd in my library?`
- `Best 5 Bjork albums`
- `Best 5 Bjork albums in my library`
- `Give me three records for a rainy late-night walk.`
- `Give me three records for a rainy late-night walk, but only from my library.`
- `Find me some melancholic dream pop albums in my library.`
- `Which of those have I played recently?` after the prior prompt in the same session
- `Make me a melancholy jazz playlist for late nights.`
- `Make me a playlist`
- `Add five colder tracks to the existing playlist Melancholy Jazz`
- `Hi again.` after discarding a pending action in the same session
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

## Tool Manifest Mode

The service now supports two tool-manifest modes:

- `routed` (default): compact static system prompt plus a per-turn routed tool manifest for likely categories
- `full`: inject the full tool manifest every turn as a compatibility fallback

To test `routed`:

1. Set `AGENT_TOOL_MANIFEST_MODE=routed` in `.env` or leave it unset
2. rebuild and restart the service
3. run the regression prompts and compare prompt-token usage in `docker logs -f groovarr`

To test `full`:

1. Set `AGENT_TOOL_MANIFEST_MODE=full` in `.env`
2. rebuild and restart the service
3. rerun the same prompts and compare behavior and prompt-token usage

To roll back quickly if routed manifests behave worse:

1. set `AGENT_TOOL_MANIFEST_MODE=full`
2. rebuild `groovarr`
3. rerun `/api/health` and a small chat smoke set

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
