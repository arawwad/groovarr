# Follow-Up Resilience TODO

## Priority 0

- Add a canonical `active focus` object to session memory so follow-ups bind to a concrete conversational object, not only the latest matching cache.
- Persist `active focus` for empty result sets, not just non-empty result caches.
- Use `active focus` during turn resolution before final routing so terse follow-ups can stay in-domain.

## Priority 1

- Make empty badly-rated follow-ups stay bound to the badly-rated cleanup flow instead of drifting into playlist or general recommendation routes.
- Treat pending playlist previews as editable draft objects. Refinements should mutate the draft preview, not refresh a persisted Navidrome playlist.
- Make exact inventory lookup mode sticky across short follow-ups like `What about The Bends by Radiohead?`.

## Priority 2

- Preserve clarification dimensions across turns, especially `artist stats` versus `listening summary`. Done for stats scope carryover and artist-catalog follow-ups over prior artist stats results.
- Keep inherited constraints attached to the active focus, including artist filters, `libraryOnly`, and time windows. Partially done for creative album result sets.
- Tighten album recommendation execution so explicit artist constraints do not leak during deterministic routing. Done for artist-filtered follow-ups over active album result sets.

## Priority 3

- Add declared follow-up operations per focus kind, for example `playlist_draft -> refine_style`, `artist_candidates -> revisit_pick`, `badly_rated_albums -> preview_apply`.
- Expose `active focus` in debug/turn output so misbindings are visible in archives.
- Add a conversation-level regression suite that exercises natural follow-up flows instead of isolated single-turn route tests.
