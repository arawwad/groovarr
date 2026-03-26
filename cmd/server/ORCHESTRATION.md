# Chat Orchestration Contracts

This document defines the staged chat contract for `cmd/server`.

The goal is simple:
- semantic understanding should be model-driven
- grounding, state resolution, and safety should be deterministic
- executors should route from structured state, not from raw wording

## Stages

The live chat path should be read as:

1. `normalizer`
   - input: raw user message, recent history, server session context
   - output: `normalizedTurn`
2. `sanitizer`
   - input: `normalizedTurn`
   - output: validated `normalizedTurn`
3. `context resolver`
   - input: validated `normalizedTurn`, session caches
   - output: `resolvedTurnContext`
4. `planner`
   - input: raw message, history, `resolvedTurnContext`
   - output: `orchestrationDecision`
5. `executor`
   - input: `orchestrationDecision`, `resolvedTurnContext`
   - output: deterministic response or agent handoff
6. `responder`
   - input: agent/tool results plus structured turn signals
   - output: final user-facing reply

Rule: only stages 1 and 4 should interpret raw language. Later stages should consume structured fields.

## `normalizedTurn`

This is the front-door semantic contract.

Current fields:
- `intent`
  - `album_discovery | track_discovery | artist_discovery | scene_discovery | listening | stats | playlist | general_chat | other`
- `subIntent`
  - narrower operation within an intent
- `styleHints`
  - short refinement cues for creative follow-ups
- `followupMode`
  - `none | refine_previous | query_previous_set | pivot`
- `queryScope`
  - `general | library | listening | stats | playlist | unknown`
- `libraryOnly`
  - explicit ownership/library constraint
- `timeWindow`
  - `none | last_month | this_month | this_year | explicit | ambiguous_recent`
- `resultSetKind`
  - `none | creative_albums | semantic_albums | discovered_albums | cleanup_candidates | badly_rated_albums | playlist_candidates | recent_listening | scene_candidates | song_path | track_candidates | artist_candidates`
- `resultAction`
  - `none | inspect_availability | preview_apply | apply_confirmed | compare | filter_by_play_window | pick_riskier | refine_style | select_candidate | describe_item`
- `selectionMode`
  - `none | all | top_n | ordinal | explicit_names | missing_only | count_match`
- `selectionValue`
  - compact selection payload
- `compareSelectionMode`
- `compareSelectionValue`
- `targetName`
  - explicit playlist or other named target when the user states it directly
- `artistName`
- `trackTitle`
- `promptHint`
  - short append/refine prompt for playlist-style modifications
- `needsClarification`
- `clarificationFocus`
  - `none | scope | time_window | target_type | reference | other`
- `referenceTarget`
  - `none | previous_results | previous_taste | previous_playlist | previous_stats`
- `referenceQualifier`
  - `none | latest_set | last_item | safer | riskier`
- `confidence`
  - `low | medium | high`
- `clarificationPrompt`

Current supported `subIntent` values:
- `listening_summary`
- `listening_interpretation`
- `result_set_play_recency`
- `result_set_most_recent`
- `artist_dominance`
- `library_top_artists`
- `creative_refinement`
- `creative_risk_pick`
- `creative_safe_pick`
- `scene_overview`
- `playlist_tracks_query`
- `playlist_availability`
- `playlist_append`
- `playlist_refresh`
- `playlist_repair`
- `playlist_vibe`
- `playlist_artist_coverage`
- `playlist_queue_request`
- `track_search`
- `track_similarity`
- `track_description`
- `song_path_summary`
- `artist_similarity`
- `artist_starting_album`
- `lidarr_cleanup_apply`
- `badly_rated_cleanup`
- `artist_remove`

## `normalizedTurn` invariants

The sanitizer owns these rules:
- `libraryOnly=true` implies `queryScope=library`
- follow-up turns without an explicit target default `referenceTarget=previous_results`
- `ambiguous_recent` should default instead of over-clarifying for listening/stats turns
- unsupported enums are downgraded to safe defaults
- clarification text is ignored unless `needsClarification=true`
- cleanup and badly-rated follow-ups infer their result set kind from cached session state when the turn is clearly about a prior preview

The normalizer should not invent factual constraints. It may classify semantics, but it should not fabricate ownership, time windows, or references.

## Generic Result Reference

The normalized contract now has a generic wrapper in code:

- `resultReference`
  - `setKind`
  - `action`
  - `selection.mode`
  - `selection.value`
  - `target`
  - `qualifier`
- `resolvedResultReference`
  - all of the above plus:
  - `resolvedSetKind`
  - `resolvedSource`
  - `resolvedItemKey`
  - `resolvedItemRef`
  - `ambiguous`

This is intentionally an internal orchestration contract layered on top of `normalizedTurn`.

Rule:
- executors should consume `resultReference` / `resolvedResultReference`
- not a scattered mix of `ResultSetKind`, `ResultAction`, `SelectionMode`, `SelectionValue`, and ad hoc resolved fields

This keeps the parser wire format stable while moving server logic toward one generic result-set reference model.

## `serverTurnRequest`

This is the server-compatible structured request that later stages should converge on.

It is built from `resolvedTurnContext` and is the shared shape used for:
- planner context
- responder context
- orchestration/resolver agent handoff

Current fields:
- top-level turn semantics
  - `intent`
  - `subIntent`
  - `followupMode`
  - `queryScope`
  - `timeWindow`
  - `confidence`
  - `libraryOnly`
  - `needsClarification`
  - `clarificationFocus`
  - `clarificationPrompt`
- user-directed modifiers
  - `styleHints`
  - `targetName`
  - `artistName`
  - `trackTitle`
  - `promptHint`
- `reference`
  - `target`
  - `qualifier`
  - `requestedSet`
  - `resolvedSet`
  - `resolvedSource`
  - `resolvedItemKey`
  - `resolvedItemRef`
  - `missingContext`
  - `ambiguous`
- `workflow`
  - `action`
  - `selectionMode`
  - `selectionValue`
  - `compareSelectionMode`
  - `compareSelectionValue`
- `session`
  - result-set availability flags

Rule:
- new orchestration work should target `serverTurnRequest`
- not raw `normalizedTurn`
- not hand-built prompt strings that reserialize the same state differently per stage

## Stage Contracts

These contracts are now defined in code. The live planner path uses `orchestrationDecision`, and the live resolver path uses `resultSetResolverRequest -> resultSetResolverDecision -> serverExecutionRequest`.

### `orchestrationDecision`

This is the intended output for the orchestration agent.

Fields:
- `nextStage`
  - `clarify | deterministic | resolver | responder`
- `deterministicMode`
- `clarificationPrompt`
- `reason`
- `confidence`

Rule:
- this agent decides what stage runs next
- it does not resolve selections or execute operations

### `resultSetResolverRequest`

This is the intended input to the result-set resolver agent.

Fields:
- `turn`
  - the full `serverTurnRequest`
- `capabilities`
  - per-result-set supported operations and selectors

Rule:
- capabilities should be owned by the domain or result-set adapter that implements them
- do not keep a drifting central list of capabilities detached from execution

### `resultSetResolverDecision`

This is the intended output of the result-set resolver agent.

Fields:
- `setKind`
- `itemKey`
- `operation`
- `selectionMode`
- `selectionValue`
- `compareSelectionMode`
- `compareSelectionValue`
- `needsClarification`
- `clarificationPrompt`
- `reason`
- `confidence`

Rule:
- this agent should translate conversational references into a server-compatible operation request
- it should not produce the final user-facing reply

### `serverExecutionRequest`

This is the deterministic execution contract the server should converge on.

Fields:
- `domain`
- `setKind`
- `operation`
- `selectionMode`
- `selectionValue`
- `compareSelectionMode`
- `compareSelectionValue`
- `itemKey`
- `targetName`
- `artistName`
- `trackTitle`
- `promptHint`
- `timeWindow`

Rule:
- executor refactors should consume this shape instead of raw turn fields plus ad hoc helper state

## Current Notes

- Compare-style follow-ups on prior result sets are intended to go through the `resolver` stage instead of generic deterministic-first routing.
- The compare contract is live and now honors explicit primary selectors or focused-item references for the anchor item, not just the comparison target.
- `song_path` memory can now feed later `track_discovery` follow-ups when the user refers to the midpoint or last focused item from a previously returned path.
- The remaining edge is richer composite follow-ups where a selector, comparison target, and secondary action are all implied in one short turn.

## `resolvedTurnContext`

This binds the semantic turn to actual session state.

Current responsibilities:
- detect whether the session has:
  - creative album set
  - semantic album set
  - discovered albums
  - cleanup candidates
  - badly rated albums
  - recent listening summary
  - pending playlist plan
  - resolved scene
- mark `MissingReferenceContext` when a follow-up references state that does not exist
- escalate missing reference into clarification
- bind `previous_results` to a specific cached result-set kind using explicit contract hints first, then recency-based precedence
- clarify when two plausible cached sets are effectively tied and the contract does not disambiguate them

Current resolved fields:
- `ResolvedReferenceKind`
- `ResolvedReferenceSource`
- `ResolvedItemKey`
- `ResolvedItemSource`
- `AmbiguousReference`

Rule: context resolution decides whether a reference can be grounded. It does not reinterpret the user message.

Current precedence rule:
- explicit `resultSetKind` wins
- explicit `referenceTarget` narrows the eligible cache family
- otherwise the resolver chooses the most recent eligible cached set
- if two eligible cached sets are effectively tied, the resolver asks for clarification instead of guessing

## `orchestrationDecision`

This is the live routing contract between orchestration and execution.

Current fields:
- `nextStage`
  - `clarify | deterministic | resolver | responder`
- `deterministicMode`
  - `normalized_first | none`
- `clarificationPrompt`
- `reason`
- `confidence`

Current planner responsibilities:
- short-circuit clarification when the normalized turn already requires it
- choose deterministic routing for stable covered paths
- use `resolver` for result-set resolution before execution when the next step depends on grounded set/item selection
- hand off to the responder path for open-ended or weakly grounded requests

Planner rule: the planner chooses *what stage runs next*, not *what the user meant*. That belongs to the normalizer.

## `agent.TurnSignals`

This is the structured server-to-agent handoff.

Current fields:
- `Intent`
- `QueryScope`
- `FollowupMode`
- `LibraryOnly`
- session-state availability flags

Current limitation:
- the agent signal contract is coarser than `normalizedTurn`
- it does not yet carry `SubIntent`, `ReferenceTarget`, `TimeWindow`, or workflow-selection intent

This is acceptable for now because the server owns more routing than before, but it is not the end state.

## Ownership by stage

`normalizer`
- semantic classification
- paraphrase handling
- typo-tolerant coarse extraction

`sanitizer`
- enum cleanup
- safe defaults
- impossible-combination repair

`execution handlers`
- domain-owned dispatch from `serverExecutionRequest`
- deterministic execution only
- capability ownership should stay aligned with the same domains

`context resolver`
- bind references to real session caches
- reject missing references

`planner`
- choose `clarify | deterministic | agent`

`deterministic executors`
- execute only from structured fields and session state
- never add new raw-text routing shortcuts when the contract is too weak

`agent`
- natural language response
- tool use for non-deterministic cases
- fallback when deterministic coverage is insufficient

## Remaining contract pieces

The discovered-result workflow fields now exist in the semantic contract. The remaining work is making every relevant executor consume them cleanly, especially for follow-ups like:
- "queue the first two"
- "are any of those already monitored?"
- "apply only the missing ones"
- "the second and fourth"

These should not be solved with phrase matching. The current contract surface for them is:

- `resultSetKind`
  - `none | creative_albums | semantic_albums | discovered_albums | playlist_candidates | recent_listening`
- `resultAction`
  - `none | inspect_availability | preview_apply | apply_confirmed | compare | filter_by_play_window | pick_riskier | refine_style`
- `selectionMode`
  - `none | all | top_n | ordinal | explicit_names | missing_only`
- `selectionValue`
  - compact payload for `top_n`, ordinal positions, or explicit labels

`missing_only` is now implemented for discovered-album availability and preview-apply follow-ups. The remaining gap in this area is richer combined selection semantics if we ever want phrases like "first three missing ones" as one structured operation.

Scene selection also now uses the same contract surface:
- `resultSetKind=scene_candidates`
- `resultAction=select_candidate`
- `selectionMode=ordinal | explicit_names | count_match`

Playlist introspection is starting to use the same contract surface:
- `subIntent=playlist_inventory`
- `subIntent=playlist_tracks_query`
- `subIntent=playlist_availability`
- `subIntent=playlist_vibe`
- `subIntent=playlist_artist_coverage`
- `targetName` for explicit playlist names
- `artistName` for explicit artist coverage checks

Playlist modification flows are also starting to use the same contract surface:
- `subIntent=playlist_append | playlist_refresh | playlist_repair | playlist_queue_request`
- `targetName` or `referenceTarget=previous_playlist`
- `promptHint` for append-style changes

Cleanup flows now use the same contract surface:
- `subIntent=lidarr_cleanup_apply`
- `subIntent=badly_rated_cleanup`
- `subIntent=artist_remove`
- `resultSetKind=cleanup_candidates | badly_rated_albums`
- `resultAction=preview_apply`
- `artistName` for explicit artist removal

## Contract expansion rule

When a new conversational pattern cannot be executed cleanly from existing structured fields:
- do not add a new raw-text helper in the executor
- first decide whether the pattern is:
  - a new `subIntent`
  - a new `resultAction`
  - a new `selectionMode`
  - or a missing session-state concept

If that is unclear, stop and decide the contract explicitly before coding.

## Near-term target state

The next clean milestone is:
- keep the current normalizer/planner structure
- add result-set workflow fields for discovered album follow-ups
- expand `agent.TurnSignals` only when the server can no longer keep the routing decision local
- add an audit stage later if unsupported claims remain a problem

That keeps the architecture staged without multiplying agents prematurely.
