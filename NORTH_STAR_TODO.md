# North Star TODO

This is the execution checklist for building Groovarr into a serious music-intelligence stack without drifting.

## Rules

- Do not start a new phase until the current phase exit criteria are met.
- Do not add new features unless they improve similarity, ranking, playlists, discovery, or collection decisions.
- Keep Navidrome as the playback surface, Groovarr as the control plane, and AudioMuse as an optional sonic engine behind Groovarr.
- Do not force rich Groovarr controls into Navidrome unless the Navidrome client can consume them cleanly.
- Every completed item should leave behind code, tests, docs, or a measurable metric.

## North Star

Build a local-first music intelligence platform where:

- Groovarr owns ranking, workflows, taste modeling, and API contracts.
- AudioMuse provides sonic similarity and future sonic exploration features.
- Navidrome remains the user-facing playback surface.
- Lidarr remains the library acquisition and apply layer.

## Consumption Model

- Navidrome should consume opinionated defaults for `Instant Mix`, `similar songs`, and `similar artists`.
- Groovarr should own explicit mode selection, mood/context overrides, explanations, playlist controls, and discovery workflows.
- Not every Groovarr capability needs to be directly user-selectable from Navidrome.
- If a control is hard to express in Navidrome, expose it in Groovarr and let Navidrome consume the result.
- For the current MVP, assume a single-user system and use one global active listening context.

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

### Product Boundary

- Recommendation modes are a Groovarr concept first.
- Navidrome should get stable default behavior per action, not a complex mode picker.
- Runtime mode or mood changes should happen through Groovarr APIs and affect Navidrome plugin results indirectly.
- `mode` and `mood` are different controls:
  - `mode` = recommendation policy
  - `mood` = desired vibe or musical character
- For the MVP:
  - `mode` should be a constrained enum/dropdown
  - `mood` should be open text
- Both should affect Navidrome `Instant Mix` and related plugin-driven similarity in real time through Groovarr context resolution.

### TODO

- [x] Define a persistent taste-profile schema in Postgres.
- [x] Derive compact taste features from already-synced Navidrome signals instead of duplicating raw history.
- [x] Add profile derivation from these signals:
  - [x] play count
  - [x] recency
  - [x] favorites/ratings
  - [ ] playlist saves
  - [x] repeated plays
- [x] Add ranking-feature groundwork for:
  - [x] artist familiarity
  - [x] artist fatigue
  - [x] album overexposure
  - [x] novelty tolerance
  - [x] replay affinity
- [x] Add a reranker that combines:
  - [x] AudioMuse score
  - [x] local similarity score
  - [x] listening-affinity score
  - [x] diversity penalty
- [ ] Add explicit recommendation modes:
  - [ ] `familiar`
  - [ ] `adjacent`
  - [ ] `deep-cut`
  - [ ] `surprise`
  - [ ] `library-only`
- [x] Treat `mode` as a closed enum in Groovarr UI/API, not free text.
- [x] Treat `mood` as a free-text listening-vibe input, not a policy enum.
- [x] Define the default mode per Navidrome action:
  - [x] `Instant Mix from track -> adjacent`
  - [x] `similar artists -> adjacent`
  - [x] `artist radio / similar songs by artist -> familiar or adjacent`
- [x] Expose mode selection through Groovarr similarity APIs.
- [x] Add a runtime listening-context API for mode and mood overrides:
  - [x] `POST /api/similarity/context`
  - [x] `GET /api/similarity/context`
  - [x] `DELETE /api/similarity/context`
- [x] Implement the MVP as a single global context record:
  - [x] `mode`
  - [x] `mood`
  - [x] `expires_at`
  - [x] `updated_at`
  - [x] optional `source`
- [x] Resolve plugin requests against the active global context with optional TTL expiry.
- [ ] Make plugin requests resolve precedence in this order:
  - [ ] explicit request override
  - [ ] active runtime context
  - [ ] plugin default
  - [ ] system default
- [ ] Keep Navidrome UI simple by avoiding a direct mode or mood picker unless there is a clean client surface for it.
- [x] Add tests for reranking behavior.
- [ ] Add explanations for why a result was chosen.

### Exit Criteria

- Same seed produces materially different results by mode.
- Ranking quality clearly improves over raw AudioMuse/local candidates.
- Explanations reference real library or listening signals.
- Navidrome gets better default behavior without needing rich in-client controls.
- Changing the global listening context in Groovarr affects Navidrome `Instant Mix` behavior in real time.

## Phase 3: Playlist Intelligence

### Goal

Turn the existing playlist workflow into a more intelligent, easier-to-use tool layer with controls, continuity, and maintenance.

### Product Boundary

- Advanced controls belong in Groovarr, not Navidrome.
- Navidrome can consume playlists or radios generated by Groovarr even when the configuration happened elsewhere.
- Phase 3 should land first as Groovarr LLM tools and approval flows, not as a separate non-chat product lane.
- New playlist capabilities should be callable by the agent, previewable in chat, and optionally surfaced in UI later.
- Avoid building a second orchestration path for playlists if the same capability can be expressed as a tool plus approval flow.
- Do not replace or break the current tested playlist workflow.
- Treat the current playlist tools as the stable primitive layer.
- Phase 3 should add a higher-level tool layer over the existing primitives, not invent a separate playlist engine.
- Phase 3 is different from Phase 2:
  - Phase 2 serves a specific Navidrome playback feature with explicit runtime controls.
  - Phase 3 should be conversational first, with zero required configuration for normal playlist requests.
- For playlist UX:
  - prefer natural requests such as "make me a playlist" or "add to this playlist"
  - keep hard settings minimal
  - let the LLM ask a concise clarifying question only when a required target is missing or ambiguous
- Do not promote operator-style controls to primary UX if a good default is possible.

### TODO

- [x] Keep the current tested playlist workflow unchanged while adding Phase 3 enhancements on top.
- [ ] Keep the current playlist primitives available as the stable base:
  - [ ] `planDiscoverPlaylist`
  - [ ] `resolvePlaylistTracks`
  - [ ] `queueMissingPlaylistTracks`
  - [ ] `createDiscoveredPlaylist`
  - [ ] `navidromePlaylists`
  - [ ] `navidromePlaylist`
  - [ ] `navidromePlaylistState`
  - [ ] `addTrackToNavidromePlaylist`
  - [ ] `queueTrackForNavidromePlaylist`
  - [ ] `removeTrackFromNavidromePlaylist`
  - [ ] `removePendingTracksFromNavidromePlaylist`
- [ ] Use high-level playlist tools as the preferred conversational layer:
  - [ ] `startPlaylistCreatePreview`
  - [ ] `startPlaylistAppendPreview`
- [ ] Keep hard settings minimal and zero-config by default for normal playlist requests.
- [ ] Ask a concise clarifying question only when required information is missing.

### Chunk 1 Gate: Conversational Create And Append

- [ ] `TESTING.md` Playlist Create Preview E2E passes.
- [ ] `TESTING.md` Playlist Append Preview E2E passes.
- [ ] `TESTING.md` Pending Action Approve / Discard E2E passes for playlist previews.
- [ ] `TESTING.md` Conversational Playlist Clarification Check passes.
- [ ] The agent prefers `startPlaylistCreatePreview` for normal "make me a playlist" requests.
- [ ] The agent prefers `startPlaylistAppendPreview` for normal "add to this playlist" requests.
- [ ] No advanced settings are required for normal create/append requests.
- [ ] Shared preview shape is stable for create and append:
  - [ ] `response`
  - [ ] `pendingAction`
  - [ ] `playlistName`
  - [ ] `mode`
  - [ ] `counts`
  - [ ] `tracks`
- [ ] Approval applies the reviewed plan rather than silently recomputing a different one.
- [ ] Chunk 1 is green before any new Phase 3 scope is started.

### Chunk 2 Gate: Better Playlist Quality

- [ ] Add sequencing improvements for create and append:
  - [ ] mood continuity
  - [ ] energy drift
  - [x] anti-clumping by artist
  - [ ] anti-clumping by album
- [ ] Add explanations for why tracks were chosen.
- [ ] Re-run Chunk 1 E2E checks and keep them passing.
- [ ] Add at least one measurable playlist quality metric.

### Chunk 3 Gate: Refresh And Repair

- [x] Add explicit E2E tests for refresh.
- [x] Add explicit E2E tests for repair.
- [x] `startPlaylistRefreshPreview` is conversational first with zero-config defaults.
- [x] `startPlaylistRepairPreview` is conversational first with zero-config defaults.
- [x] Refresh can still target later stale slots even when a playlist is structurally clean.
- [x] Repair can remove irreparable broken/duplicate slots instead of failing the whole preview.
- [x] Refresh and repair pass their own E2E checks before any extra tuning or options are added.

### Deferred Until Proven Necessary

- [ ] Add advanced inputs only if real usage proves zero-config defaults are insufficient:
  - [ ] seed track
  - [ ] seed artist
  - [ ] seed album
  - [ ] source playlist
- [ ] Add explicit constraints only if real usage proves they are necessary:
  - [ ] artist cap
  - [ ] decade balance
  - [ ] genre spread
  - [ ] recency filter
  - [ ] library-only mode
  - [ ] explicit length target
- [ ] Add adaptive feedback loop from:
  - [ ] replays
  - [ ] removals
  - [ ] saves
  - [ ] manual edits

### Exit Criteria

- Playlists are meaningfully better than raw nearest-neighbor dumps.
- Users can generate and refine playlists through Groovarr with confidence.
- Navidrome can consume Groovarr-generated playlist outputs without needing Groovarr-level controls in its UI.
- Playlist generation, append, refresh, and repair are all available through Groovarr agent tools with approval where needed.
- The current tested playlist primitives still work and remain available underneath the higher-level tool layer.

## Phase 4: Discovery Intelligence

### Goal

Use the taste model and similarity graph to grow the library deliberately.

### Product Boundary

- Phase 4 should extend Groovarr’s agent/tool system, not fork into a separate discovery product flow.
- Discovery candidates should be generated, explained, previewed, and approved through chat/tools first.
- Lidarr, downloader, and importer actions should remain tool-driven with explicit approval boundaries.
- UI surfaces can summarize or accelerate discovery later, but the first-class contract should be agent tools.

### TODO

- [ ] Add LLM tools for discovery expansion, discovery explanation, and acquisition preview.
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
- Discovery generation, review, and apply-preview all work through Groovarr tools without introducing a separate orchestration stack.

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
