# Navidrome Plugin API

This file defines the Groovarr-side contract for a thin Navidrome plugin.

## Goal

Keep the Navidrome plugin minimal.

- The plugin should only translate Navidrome hook calls into HTTP requests.
- Groovarr should own provider selection, AudioMuse fallback, ranking, filtering, and library mapping.

## Base URLs

- Groovarr API: `http://groovarr:8088`
- Health: `GET /api/similarity/health`

## Track Similarity

`POST /api/similarity/tracks`

Request:

```json
{
  "seedTrackId": "navidrome-track-id",
  "seedTrackTitle": "",
  "seedArtistName": "",
  "provider": "hybrid",
  "limit": 25,
  "excludeRecentDays": 14,
  "excludeSeedArtist": false
}
```

Notes:

- Prefer `seedTrackId`.
- Use `seedTrackTitle` + `seedArtistName` only when a Navidrome hook does not provide the track ID.
- `provider` may be `local`, `audiomuse`, or `hybrid`.

Response:

```json
{
  "similarityTracks": {
    "provider": "hybrid",
    "seed": {
      "id": "123",
      "title": "Seed Track",
      "artistName": "Seed Artist"
    },
    "results": [
      {
        "id": "456",
        "albumId": "789",
        "title": "Result Track",
        "artistName": "Result Artist",
        "rating": 4,
        "playCount": 12,
        "lastPlayed": "2026-03-12T16:00:00Z",
        "score": 0.91,
        "sourceScores": {
          "local": 0.84,
          "audiomuse": 0.96
        },
        "sources": ["audiomuse", "local"]
      }
    ]
  }
}
```

Plugin mapping:

- Return each `results[].id` as the Navidrome song reference ID.
- Ignore entries without `id`.

## Similar Songs By Artist

`POST /api/similarity/songs/by-artist`

Request:

```json
{
  "seedArtistName": "Radiohead",
  "provider": "hybrid",
  "limit": 25,
  "excludeRecentDays": 14,
  "excludeSeedArtist": false
}
```

Response:

```json
{
  "similaritySongsByArtist": {
    "provider": "hybrid",
    "seed": {
      "name": "Radiohead"
    },
    "results": [
      {
        "id": "456",
        "albumId": "789",
        "title": "Result Track",
        "artistName": "Result Artist",
        "score": 0.87,
        "sourceScores": {
          "local": 0.79,
          "audiomuse": 0.93
        },
        "sources": ["audiomuse", "local"]
      }
    ]
  }
}
```

## Artist Similarity

`POST /api/similarity/artists`

Request:

```json
{
  "seedArtistName": "Radiohead",
  "provider": "hybrid",
  "limit": 10
}
```

Response:

```json
{
  "similarityArtists": {
    "provider": "hybrid",
    "seed": {
      "name": "Radiohead"
    },
    "results": [
      {
        "id": "artist-id",
        "name": "Portishead",
        "rating": 5,
        "playCount": 43,
        "score": 0.88,
        "sourceScores": {
          "local": 0.73,
          "audiomuse": 0.99
        },
        "sources": ["audiomuse", "local"]
      }
    ]
  }
}
```

Plugin mapping:

- Return each `results[].id` as the Navidrome artist reference ID when present.
- Fall back to name-only rendering when an artist ID is not available.

## Health Probe

`GET /api/similarity/health`

Example response:

```json
{
  "similarity": {
    "defaultProvider": "hybrid",
    "audioMuseConfigured": true,
    "audioMuseReachable": true,
    "availableProviders": ["local", "audiomuse", "hybrid"],
    "preferredTrackSource": "hybrid"
  }
}
```

## Plugin Behavior

Recommended plugin defaults:

- Similar tracks: `provider=hybrid`
- Similar songs by artist: `provider=hybrid`
- Similar artists: `provider=hybrid`
- Fallback behavior: if Groovarr is unavailable, return an empty result rather than blocking Navidrome

Recommended plugin config surface:

- `groovarrUrl`
- `provider`
- `excludeRecentDays`
- `excludeSeedArtist`
- `requestTimeoutSeconds`

## Current Implementation Status

Implemented in Groovarr:

- similarity service with `local`, `audiomuse`, and `hybrid` providers
- HTTP endpoints for track similarity, similar songs by artist, and artist similarity
- AudioMuse health probing and fallback to local similarity

Implemented here:

- the WASM Navidrome plugin source at `navidrome-plugin/`

Not yet implemented here:

- direct Song Path / Music Map / Song Alchemy adapters
