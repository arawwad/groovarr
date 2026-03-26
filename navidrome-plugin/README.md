# Groovarr Navidrome Plugin

This plugin is a thin Navidrome WASM adapter over Groovarr's similarity API.

It implements:

- similar songs by track
- similar songs by artist
- similar artists

Groovarr remains the control plane:

- provider selection (`local`, `audiomuse`, `hybrid`)
- AudioMuse fallback
- library mapping
- ranking and filtering

## Build

Requirements:

- Go 1.25+
- a reachable copy of the Navidrome Go plugin PDK during `go mod tidy`

Commands:

```bash
cd groovarr/navidrome-plugin
go mod tidy
GOOS=wasip1 GOARCH=wasm go build -o plugin.wasm ./...
make package
```

The result is `groovarrsimilarity.ndp`, containing the required `plugin.wasm` payload.

## Install

1. Copy `groovarrsimilarity.ndp` into Navidrome's plugins folder.
2. Ensure Navidrome has plugins enabled.
3. In the plugin config UI, set:
   - `groovarr_url`
   - `provider`
   - `exclude_recent_days`
   - `exclude_seed_artist`

## Backend contract

The plugin expects these Groovarr endpoints:

- `POST /api/similarity/tracks`
- `POST /api/similarity/songs/by-artist`
- `POST /api/similarity/artists`
- `GET /api/similarity/health`

The detailed contract is documented in [../NAVIDROME_PLUGIN_API.md](../NAVIDROME_PLUGIN_API.md).
