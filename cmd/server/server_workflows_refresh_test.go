package main

import "testing"

func TestSelectPlaylistRefreshEntriesPrefersRepeatedClustersOverOpener(t *testing.T) {
	entries := []navidromePlaylistEntry{
		{ID: "1", Artist: "Miles Davis", Title: "Blue in Green"},
		{ID: "2", Artist: "Bill Evans", Title: "Peace Piece"},
		{ID: "3", Artist: "Chet Baker", Title: "Almost Blue"},
		{ID: "4", Artist: "Chet Baker", Title: "My Funny Valentine"},
		{ID: "5", Artist: "Nils Frahm", Title: "Says"},
		{ID: "6", Artist: "Bohren & der Club of Gore", Title: "Prowler"},
		{ID: "7", Artist: "Bohren & der Club of Gore", Title: "Midnight Walker"},
	}

	indexes, selected := selectPlaylistRefreshEntries(entries, 2)
	if len(indexes) != 2 || len(selected) != 2 {
		t.Fatalf("got %d indexes and %d entries, want 2 each", len(indexes), len(selected))
	}
	if indexes[0] == 0 || indexes[1] == 0 {
		t.Fatalf("refresh should not target the opener when stronger stale clusters exist: %v", indexes)
	}
	if indexes[0] < 2 {
		t.Fatalf("refresh should prefer later stale slots over early anchors: %v", indexes)
	}
	if indexes[1] < 4 {
		t.Fatalf("refresh should target a later repeated cluster, got %v", indexes)
	}
}

func TestBuildOrderedPlaylistSongIDsPreservesSlots(t *testing.T) {
	entries := []navidromePlaylistEntry{
		{ID: "1", Artist: "A", Title: "One"},
		{ID: "2", Artist: "B", Title: "Two"},
		{ID: "3", Artist: "C", Title: "Three"},
		{ID: "4", Artist: "D", Title: "Four"},
	}

	got, ok := buildOrderedPlaylistSongIDs(entries, []int{1, 3}, []string{"9", "10"})
	if !ok {
		t.Fatal("expected ordered playlist rebuild to succeed")
	}
	want := []string{"1", "9", "3", "10"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildOrderedPlaylistSongIDsAllowsRemovalOnly(t *testing.T) {
	entries := []navidromePlaylistEntry{
		{ID: "1", Artist: "A", Title: "One"},
		{ID: "2", Artist: "B", Title: "Two"},
		{ID: "3", Artist: "C", Title: "Three"},
	}

	got, ok := buildOrderedPlaylistSongIDs(entries, []int{1}, nil)
	if !ok {
		t.Fatal("expected removal-only ordered rebuild to succeed")
	}
	want := []string{"1", "3"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

func TestSelectPlaylistRepairIssuesFindsBrokenAndDuplicateEntries(t *testing.T) {
	entries := []navidromePlaylistEntry{
		{ID: "1", Artist: "A", Title: "One"},
		{ID: "2", Artist: "B", Title: "Two"},
		{ID: "2", Artist: "B", Title: "Two"},
		{ID: "", Artist: "C", Title: "Three"},
		{ID: "", Artist: "", Title: ""},
	}

	issues := selectPlaylistRepairIssues(entries)
	if len(issues) != 3 {
		t.Fatalf("len(issues) = %d, want 3", len(issues))
	}
	if issues[0].Index != 2 || issues[0].Reason == "" {
		t.Fatalf("first issue = %+v, want duplicate track at index 2", issues[0])
	}
	if issues[1].Index != 3 || issues[1].Reason != "the entry is missing a track id" {
		t.Fatalf("second issue = %+v", issues[1])
	}
	if issues[2].Index != 4 || issues[2].Reason != "the entry is missing both a track id and usable artist/title metadata" {
		t.Fatalf("third issue = %+v", issues[2])
	}
}

func TestSequencePlaylistAvailableAdditionsAvoidsImmediateArtistRepeat(t *testing.T) {
	resolved := []resolvedPlaylistTrack{
		{SongID: "1", ArtistName: "Miles Davis", TrackTitle: "A", Status: "available"},
		{SongID: "2", ArtistName: "Miles Davis", TrackTitle: "B", Status: "available"},
		{SongID: "3", ArtistName: "Bill Evans", TrackTitle: "C", Status: "available"},
	}
	context := []navidromePlaylistEntry{
		{ID: "x", Artist: "Miles Davis", Title: "Tail Seed"},
	}

	orderedIDs, orderedTracks := sequencePlaylistAvailableAdditions(resolved, []string{"1", "2", "3"}, context)
	if len(orderedIDs) != 3 || len(orderedTracks) != 3 {
		t.Fatalf("got %d ids and %d tracks, want 3 each", len(orderedIDs), len(orderedTracks))
	}
	if orderedIDs[0] != "3" {
		t.Fatalf("first ordered id = %q, want Bill Evans track first to avoid immediate artist repeat", orderedIDs[0])
	}
	if orderedTracks[0].ArtistName != "Bill Evans" {
		t.Fatalf("first ordered track = %+v", orderedTracks[0])
	}
}

func TestSequencePlaylistSlotReplacementsAvoidsNeighborArtistRepeat(t *testing.T) {
	entries := []navidromePlaylistEntry{
		{ID: "1", Artist: "Miles Davis", Title: "One"},
		{ID: "2", Artist: "Bill Evans", Title: "Two"},
		{ID: "3", Artist: "Chet Baker", Title: "Three"},
	}
	resolved := []resolvedPlaylistTrack{
		{SongID: "10", ArtistName: "Miles Davis", TrackTitle: "Alt One", Status: "available"},
		{SongID: "11", ArtistName: "Pharoah Sanders", TrackTitle: "Alt Two", Status: "available"},
	}

	orderedIDs, orderedTracks := sequencePlaylistSlotReplacements(resolved, []string{"10", "11"}, entries, []int{1})
	if len(orderedIDs) != 1 || len(orderedTracks) != 1 {
		t.Fatalf("got %d ids and %d tracks, want 1 each", len(orderedIDs), len(orderedTracks))
	}
	if orderedIDs[0] != "11" {
		t.Fatalf("first replacement id = %q, want non-neighbor artist first", orderedIDs[0])
	}
	if orderedTracks[0].ArtistName != "Pharoah Sanders" {
		t.Fatalf("first replacement track = %+v", orderedTracks[0])
	}
}

func TestSelectPlaylistRefreshEntriesFallsBackToTailOnCleanPlaylist(t *testing.T) {
	entries := []navidromePlaylistEntry{
		{ID: "1", Artist: "A", Title: "One"},
		{ID: "2", Artist: "B", Title: "Two"},
		{ID: "3", Artist: "C", Title: "Three"},
		{ID: "4", Artist: "D", Title: "Four"},
		{ID: "5", Artist: "E", Title: "Five"},
		{ID: "6", Artist: "F", Title: "Six"},
	}

	indexes, selected := selectPlaylistRefreshEntries(entries, 2)
	if len(indexes) != 2 || len(selected) != 2 {
		t.Fatalf("got %d indexes and %d entries, want 2 each", len(indexes), len(selected))
	}
	if indexes[0] < 2 || indexes[1] < 3 {
		t.Fatalf("refresh fallback should target later clean-playlist slots, got %v", indexes)
	}
	if indexes[0] == 0 || indexes[1] == 0 {
		t.Fatalf("refresh fallback should not target opener, got %v", indexes)
	}
}
