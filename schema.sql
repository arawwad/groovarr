-- Groovarr Database Schema
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS artists (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    rating INTEGER DEFAULT 0,
    play_count INTEGER DEFAULT 0,
    embedding vector(768),
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_artists_name ON artists(name);
CREATE INDEX idx_artists_embedding ON artists USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

CREATE TABLE IF NOT EXISTS albums (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    artist_name TEXT NOT NULL,
    rating INTEGER DEFAULT 0,
    play_count INTEGER DEFAULT 0,
    last_played TIMESTAMP,
    year INTEGER,
    genre TEXT,
    embedding vector(768),
    embedding_document TEXT,
    embedding_version TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_albums_artist_name ON albums(artist_name);
CREATE INDEX idx_albums_play_count ON albums(play_count DESC);
CREATE INDEX idx_albums_rating ON albums(rating DESC);
CREATE INDEX idx_albums_embedding ON albums USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

CREATE TABLE IF NOT EXISTS tracks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    album_id TEXT REFERENCES albums(id),
    artist_name TEXT NOT NULL,
    rating INTEGER DEFAULT 0,
    play_count INTEGER DEFAULT 0,
    last_played TIMESTAMP,
    embedding vector(768),
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_tracks_album_id ON tracks(album_id);
CREATE INDEX idx_tracks_embedding ON tracks USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

CREATE TABLE IF NOT EXISTS play_events (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL,
    track_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    played_at TIMESTAMP NOT NULL,
    submission_time BIGINT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_play_events_unique ON play_events(user_id, track_id, played_at);
CREATE INDEX IF NOT EXISTS idx_play_events_played_at ON play_events(played_at DESC);
CREATE INDEX IF NOT EXISTS idx_play_events_track_id ON play_events(track_id);

CREATE TABLE IF NOT EXISTS sync_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

INSERT INTO sync_metadata (key, value) VALUES ('last_sync', '2000-01-01T00:00:00Z') ON CONFLICT DO NOTHING;
INSERT INTO sync_metadata (key, value) VALUES ('last_scrobble_submission_time', '0') ON CONFLICT DO NOTHING;
