# Server File Ownership

This package is still `main`, but the server code is split by domain so routing and workflow logic have clearer ownership.

- `chat_normalizer.go`
  - normalized-turn parser contract and session grounding
- `chat_planner.go`
  - planner/router contract ahead of the responder
- `ORCHESTRATION.md`
  - staged chat contract, ownership boundaries, and contract-expansion rules
- `server_normalized_routes.go`
  - normalized-first server route execution
- `server_playlists.go`
  - saved-playlist reads, follow-ups, append flow, and playlist availability follow-ups
- `server_discovery.go`
  - deterministic discovery, discovered-album follow-ups, semantic library album heuristics, and artist removal preview routing
- `server_stats.go`
  - deterministic stats and facet routes plus their parsing helpers
- `server_listening.go`
  - recent-listening summary route and listening-window helpers
- `server_workflows.go`
  - preview/apply workflow orchestration for playlist creation, album apply, cleanup, and artist removal
- `server_workflow_cache.go`
  - workflow dedupe/cache execution helper
- `server_route_helpers.go`
  - shared route-formatting and ownership cue helpers
- `approvals.go`
  - pending-action registration, lookup, approval, discard, and request scoping
- `llm_context.go`
  - session context injection for pending actions and discovered/planned playlist state
- `chat_session_archive.go`
  - temporary local JSONL archive plus debug endpoints for inspecting chat sessions, routes, and tools

If a new handler is domain-specific, prefer adding it to the matching `server_*.go` file or `server_normalized_routes.go` instead of rebuilding a catch-all legacy router.

If a new conversational pattern does not fit the current structured fields, update [ORCHESTRATION.md](/home/abdallah/docker/groovarr/cmd/server/ORCHESTRATION.md) before adding raw-text executor heuristics.

Useful temporary debug endpoints:
- `GET /api/debug/chat-sessions?since=7d&limit=50`
- `GET /api/debug/chat-sessions/<sessionId>?since=30d`
