# North Star TODO

This is the execution checklist for building Groovarr into a serious music-intelligence stack without drifting.

## Rules

- Do not start a new phase until the current phase exit criteria are met.
- Do not add new features unless they improve similarity, ranking, playlists, discovery, or collection decisions.
- Keep Navidrome as the playback surface, Groovarr as the control plane, and AudioMuse as an optional sonic engine behind Groovarr.
- Every completed item should leave behind code, tests, docs, or a measurable metric.

## North Star

Build a local-first music intelligence platform where:

- Groovarr owns ranking, workflows, taste modeling, and API contracts.
- AudioMuse provides sonic similarity and future sonic exploration features.
- Navidrome remains the user-facing playback surface.
- Lidarr remains the library acquisition and apply layer.

## Phase 1: Stable Hybrid Similarity

### Goal

Get production-ready hybrid similarity working through Groovarr, with AudioMuse optional and Navidrome using only Groovarr.

### TODO

- [x] Add Groovarr similarity service with `local`, `audiomuse`, and `hybrid` providers.
- [x] Add Groovarr HTTP endpoints for:
  - [x] `POST /api/similarity/tracks`
  - [x] `POST /api/similarity/songs/by-artist`
  - [x] `POST /api/similarity/artists`
  - [x] `GET /api/similarity/health`
- [x] Add DB helpers needed for seed-track resolution and track similarity.
- [x] Add tests for local and hybrid similarity flows.
- [x] Add Groovarr-backed Navidrome plugin source.
- [x] Add env contract for AudioMuse integration.
- [x] Add AudioMuse as a local service in Docker Compose.
- [x] Set `AUDIOMUSE_URL` in `groovarr/.env`.
- [x] Verify Groovarr health shows `audioMuseConfigured=true`.
- [x] Verify Groovarr health shows `audioMuseReachable=true`.
- [x] Build the Navidrome plugin `.ndp`.
- [x] Install the plugin in Navidrome.
- [x] Replace `audiomuseai` in `ND_AGENTS` with the Groovarr-backed plugin.
- [ ] Validate these user-facing flows in Navidrome:
  - [ ] similar songs by track
  - [ ] similar songs by artist
  - [ ] similar artists
- [ ] Add request/response logging for provider selection and fallback.
- [ ] Record baseline latency for local, audiomuse, and hybrid providers.

### Exit Criteria

- Navidrome uses only the Groovarr-backed plugin for similarity.
- Groovarr can serve `local` results when AudioMuse is unavailable.
- Hybrid mode returns good results and stays operational under failure.

## Phase 2: Taste-Aware Ranking

### Goal

Make similarity results feel personal instead of merely adjacent.

### TODO

- [ ] Define a persistent taste-profile schema in Postgres.
- [ ] Add ingestion of these signals into the profile:
  - [ ] play count
  - [ ] recency
  - [ ] favorites/ratings
  - [ ] playlist saves
  - [ ] repeated plays
- [ ] Add ranking features for:
  - [ ] artist familiarity
  - [ ] artist fatigue
  - [ ] album overexposure
  - [ ] novelty tolerance
  - [ ] replay affinity
- [ ] Add a reranker that combines:
  - [ ] AudioMuse score
  - [ ] local similarity score
  - [ ] listening-affinity score
  - [ ] diversity penalty
- [ ] Add explicit recommendation modes:
  - [ ] `familiar`
  - [ ] `adjacent`
  - [ ] `deep-cut`
  - [ ] `surprise`
  - [ ] `library-only`
- [ ] Expose mode selection through Groovarr similarity APIs.
- [ ] Add tests for reranking behavior.
- [ ] Add explanations for why a result was chosen.

### Exit Criteria

- Same seed produces materially different results by mode.
- Ranking quality clearly improves over raw AudioMuse/local candidates.
- Explanations reference real library or listening signals.

## Phase 3: Playlist Intelligence

### Goal

Turn similarity into usable playlists with controls, continuity, and maintenance.

### TODO

- [ ] Add playlist-generation API that accepts:
  - [ ] seed track
  - [ ] seed artist
  - [ ] seed album
  - [ ] existing playlist
  - [ ] free-text vibe
- [ ] Add playlist constraints:
  - [ ] artist cap
  - [ ] decade balance
  - [ ] genre spread
  - [ ] recency filter
  - [ ] library-only mode
  - [ ] explicit length target
- [ ] Add sequencing logic for:
  - [ ] mood continuity
  - [ ] energy drift
  - [ ] anti-clumping by artist/album
- [ ] Add playlist preview and approval flow in Groovarr.
- [ ] Add playlist refresh and repair flow for stale playlists.
- [ ] Add adaptive feedback loop from:
  - [ ] replays
  - [ ] removals
  - [ ] saves
  - [ ] manual edits
- [ ] Add playlist quality metrics.

### Exit Criteria

- Playlists are meaningfully better than raw nearest-neighbor dumps.
- Users can generate and refine playlists through Groovarr with confidence.

## Phase 4: Discovery Intelligence

### Goal

Use the taste model and similarity graph to grow the library deliberately.

### TODO

- [ ] Add discovery candidate generation from:
  - [ ] similar artists
  - [ ] similar albums
  - [ ] scene/tag neighborhoods
  - [ ] listening gaps
- [ ] Separate in-library recommendations from out-of-library discoveries.
- [ ] Add discovery explanations grounded in the user library.
- [ ] Add Groovarr workflows for:
  - [ ] preview discovered albums
  - [ ] validate against Lidarr
  - [ ] apply approved additions
- [ ] Add “missing essentials for my taste” workflow.
- [ ] Add “underexplored branch in my library” workflow.
- [ ] Track discovery conversion metrics.

### Exit Criteria

- Discovery results lead to actual Lidarr actions.
- Discovery quality is explainable and library-specific.

## Phase 5: Sonic Exploration Layer

### Goal

Add exploration features that justify local AudioMuse long term.

### TODO

- [ ] Add Music Map-style library visualization.
- [ ] Add Song Path / bridge-track generation between two tracks.
- [ ] Add cluster browsing for scenes/moods/styles.
- [ ] Add “start here” neighborhood exploration for artists and albums.
- [ ] Add support for text-to-sound exploration if AudioMuse CLAP search is viable.
- [ ] Add saved scenes or sonic collections in Groovarr.
- [ ] Add UI entrypoints for exploration outside chat.

### Exit Criteria

- Users can explore their library visually or structurally, not only through chat.
- AudioMuse is doing work Groovarr alone could not reasonably replace.

## Cross-Cutting Work

### Reliability

- [ ] Add timeouts, retries, and circuit-breaker behavior around AudioMuse calls.
- [ ] Add degraded-mode behavior when AudioMuse is slow or unavailable.
- [ ] Add health dashboard visibility for provider status.

### Metrics

- [ ] Track similarity request count by provider.
- [ ] Track fallback rate from `hybrid` to `local`.
- [ ] Track median and p95 latency by endpoint.
- [ ] Track playlist save/replay rate.
- [ ] Track discovery-to-Lidarr conversion.
- [ ] Track repeat use of similarity features.

### Quality

- [ ] Create a benchmark set of known-good seeds from your actual library.
- [ ] Compare `local`, `audiomuse`, and `hybrid` results side by side.
- [ ] Add regression tests for ranking drift.
- [ ] Add manual review checklist for “bad similarity” cases.

### Documentation

- [x] Document the Groovarr similarity API.
- [x] Document env settings for similarity providers.
- [ ] Document how to deploy AudioMuse locally in this repo.
- [ ] Document how to build/package/install the Navidrome plugin.
- [ ] Document operator runbooks for fallback/debugging.

## Immediate Next Actions

- [x] Add `audiomuse` service to Compose.
- [x] Set the correct `AUDIOMUSE_URL` in `groovarr/.env`.
- [x] Restart Groovarr and verify `/api/similarity/health`.
- [x] Build the Navidrome plugin artifact.
- [x] Install the plugin into Navidrome.
- [x] Switch Navidrome away from the existing `audiomuseai` agent.
- [ ] Run seed-track and seed-artist acceptance checks in Navidrome.

## Drift Check

Review this before starting any new work:

- Does this improve recommendation quality?
- Does this improve playlist quality?
- Does this improve discovery quality?
- Does this improve collection decisions?
- Does this reduce coupling by keeping Groovarr as the control plane?

If the answer to all five is `no`, do not start it.
