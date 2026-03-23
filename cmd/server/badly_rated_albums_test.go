package main

import (
	"strings"
	"testing"
	"time"
)

func TestSelectBadlyRatedAlbumsSupportsLeadingSelection(t *testing.T) {
	candidates := []badlyRatedAlbumCandidate{
		{AlbumName: "A"},
		{AlbumName: "B"},
		{AlbumName: "C"},
	}
	got, err := selectBadlyRatedAlbums("first 2", candidates)
	if err != nil {
		t.Fatalf("selectBadlyRatedAlbums() error = %v", err)
	}
	if len(got) != 2 || got[0].AlbumName != "A" || got[1].AlbumName != "B" {
		t.Fatalf("selectBadlyRatedAlbums() = %#v", got)
	}
}

func TestSelectBadlyRatedAlbumsSupportsOrdinalSelection(t *testing.T) {
	candidates := []badlyRatedAlbumCandidate{
		{AlbumName: "A"},
		{AlbumName: "B"},
		{AlbumName: "C"},
	}
	got, err := selectBadlyRatedAlbums("2,3", candidates)
	if err != nil {
		t.Fatalf("selectBadlyRatedAlbums() error = %v", err)
	}
	if len(got) != 2 || got[0].AlbumName != "B" || got[1].AlbumName != "C" {
		t.Fatalf("selectBadlyRatedAlbums() = %#v", got)
	}
}

func TestSelectBadlyRatedAlbumsSupportsExplicitNameSelection(t *testing.T) {
	candidates := []badlyRatedAlbumCandidate{
		{AlbumName: "Moon Safari", ArtistName: "Air"},
		{AlbumName: "Dummy", ArtistName: "Artist"},
	}
	got, err := selectBadlyRatedAlbums("moon safari", candidates)
	if err != nil {
		t.Fatalf("selectBadlyRatedAlbums() error = %v", err)
	}
	if len(got) != 1 || got[0].AlbumName != "Moon Safari" {
		t.Fatalf("selectBadlyRatedAlbums() = %#v", got)
	}
}

func TestMatchBadlyRatedAlbumsInLidarrUsesExactNormalizedArtistAndAlbum(t *testing.T) {
	matched, ambiguous, missing := matchBadlyRatedAlbumsInLidarr(
		[]badlyRatedAlbumCandidate{
			{AlbumName: "The Dark Side of the Moon", ArtistName: "Pink Floyd"},
			{AlbumName: "Heart-Shaped Box", ArtistName: "Nirvana"},
		},
		[]lidarrAlbum{
			{ID: 7, Title: "The Dark Side of the Moon", ArtistName: "Pink Floyd"},
			{ID: 8, Title: "The Dark Side of the Moon", ArtistName: "Pink Floyd"},
		},
	)
	if len(matched) != 0 {
		t.Fatalf("matched = %#v, want no exact match because title differs or is ambiguous", matched)
	}
	if len(ambiguous) != 1 || ambiguous[0].AlbumName != "The Dark Side of the Moon" {
		t.Fatalf("ambiguous = %#v", ambiguous)
	}
	if len(missing) != 1 || missing[0].AlbumName != "Heart-Shaped Box" {
		t.Fatalf("missing = %#v", missing)
	}
}

func TestFormatBadlyRatedAlbumsContextIncludesSample(t *testing.T) {
	now := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	got := formatBadlyRatedAlbumsContext(now.Add(-5*time.Minute), []badlyRatedAlbumCandidate{
		{AlbumName: "Dummy", ArtistName: "Artist", BadTrackCount: 2},
	}, now)
	if !strings.Contains(got, "last_badly_rated_albums") || !strings.Contains(got, "Dummy by Artist") {
		t.Fatalf("formatBadlyRatedAlbumsContext() = %q", got)
	}
}

func TestFormatBadlyRatedAlbumsContextIncludesZeroCount(t *testing.T) {
	now := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	got := formatBadlyRatedAlbumsContext(now.Add(-5*time.Minute), nil, now)
	if got != "last_badly_rated_albums: count=0" {
		t.Fatalf("formatBadlyRatedAlbumsContext() = %q", got)
	}
}
