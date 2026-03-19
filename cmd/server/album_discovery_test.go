package main

import (
	"errors"
	"testing"
)

func TestParseDiscoveredAlbumApplyOptionsRequiresConfirmWhenApplying(t *testing.T) {
	_, err := parseDiscoveredAlbumApplyOptions(map[string]interface{}{
		"dryRun": false,
	})
	if err == nil || err.Error() != "confirm=true is required when dryRun=false" {
		t.Fatalf("parseDiscoveredAlbumApplyOptions() error = %v", err)
	}
}

func TestPopulateMatchedAlbumAssignsStatusAndFields(t *testing.T) {
	match := newLidarrAlbumMatch(discoveredAlbumCandidate{
		Rank:       1,
		ArtistName: "Muse",
		AlbumTitle: "Absolution",
	})
	album := &lidarrAlbumSearchResult{
		ID:          42,
		Title:       "Absolution",
		ArtistID:    7,
		ArtistName:  "Muse",
		ReleaseDate: "2003-09-15",
		Monitored:   false,
	}
	album.Ratings.Value = 4.7

	got := populateMatchedAlbum(match, album, false)
	if got.Status != "can_monitor" {
		t.Fatalf("expected can_monitor, got %q", got.Status)
	}
	if got.SuggestedAction != "monitor_and_search" {
		t.Fatalf("expected monitor_and_search, got %q", got.SuggestedAction)
	}
	if got.AlbumID != 42 || got.ArtistID != 7 {
		t.Fatalf("expected ids to be copied, got album=%d artist=%d", got.AlbumID, got.ArtistID)
	}
	if got.MatchedTitle != "Absolution" || got.MatchedArtist != "Muse" {
		t.Fatalf("expected matched names to be copied, got %q / %q", got.MatchedTitle, got.MatchedArtist)
	}
}

func TestWithApplyDryRunUsesExpectedDetail(t *testing.T) {
	item := newApplyDiscoveredAlbumItem(discoveredAlbumCandidate{
		Rank:       1,
		ArtistName: "Muse",
		AlbumTitle: "Absolution",
	})
	got := withApplyDryRun(item, false, true)
	if got.Status != "dry_run" {
		t.Fatalf("expected dry_run, got %q", got.Status)
	}
	if got.Detail != "would add artist, monitor album, and trigger search" {
		t.Fatalf("unexpected dry-run detail: %q", got.Detail)
	}
}

func TestWithApplyErrorSanitizesMessage(t *testing.T) {
	item := newApplyDiscoveredAlbumItem(discoveredAlbumCandidate{
		Rank:       1,
		ArtistName: "Muse",
		AlbumTitle: "Absolution",
	})
	got := withApplyError(item, errors.New("api returned 500"))
	if got.Status != "error" {
		t.Fatalf("expected error status, got %q", got.Status)
	}
	if got.Detail != "Your library backend returned an internal error." {
		t.Fatalf("unexpected sanitized detail: %q", got.Detail)
	}
}
