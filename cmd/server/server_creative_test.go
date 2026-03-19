package main

import (
	"strings"
	"testing"
	"time"
)

func TestIsUnderplayedAlbumPrompt(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{msg: "surprise me with 3 records i own but probably underplay", want: true},
		{msg: "which albums in my library have i neglected", want: true},
		{msg: "surprise me with 3 records", want: false},
		{msg: "what have i been listening to lately", want: false},
	}
	for _, tc := range tests {
		if got := isUnderplayedAlbumPrompt(tc.msg); got != tc.want {
			t.Fatalf("isUnderplayedAlbumPrompt(%q) = %v, want %v", tc.msg, got, tc.want)
		}
	}
}

func TestRespondToCreativeAlbumRecencyFollowUpMostRecent(t *testing.T) {
	resp, ok := respondToCreativeAlbumRecencyFollowUp("which one of those have i touched most recently", []creativeAlbumCandidate{
		{Name: "Album A", ArtistName: "Artist A", LastPlayed: "2026-02-01T12:00:00Z"},
		{Name: "Album B", ArtistName: "Artist B", LastPlayed: "2026-03-01T12:00:00Z"},
	})
	if !ok {
		t.Fatal("expected recency follow-up to be handled")
	}
	if !strings.Contains(resp, "Album B by Artist B") {
		t.Fatalf("response = %q", resp)
	}
}

func TestDescribeRecentListeningDominance(t *testing.T) {
	resp := describeRecentListeningDominance(recentListeningState{
		topArtists: []recentListeningArtistState{
			{ArtistName: "Radiohead", TrackCount: 206},
			{ArtistName: "Pink Floyd", TrackCount: 61},
		},
	}, "radiohead seems dominant though right")
	if !strings.Contains(resp, "Radiohead") || !strings.Contains(resp, "206") {
		t.Fatalf("response = %q", resp)
	}
}

func TestFormatCreativeAlbumCandidateIncludesPlayContext(t *testing.T) {
	got := formatCreativeAlbumCandidate(creativeAlbumCandidate{
		Name:       "Teachings in Silence",
		ArtistName: "Ulver",
		Year:       2002,
		PlayCount:  2,
		LastPlayed: "2026-03-04T22:08:38Z",
	}, true)
	if !strings.Contains(got, "Teachings in Silence by Ulver (2002)") {
		t.Fatalf("formatCreativeAlbumCandidate() = %q", got)
	}
	if !strings.Contains(got, "plays=2") {
		t.Fatalf("formatCreativeAlbumCandidate() = %q", got)
	}
}

func TestParseCreativeAlbumTime(t *testing.T) {
	ts, ok := parseCreativeAlbumTime("2026-03-04T22:08:38Z")
	if !ok {
		t.Fatal("expected time to parse")
	}
	if ts.Year() != 2026 || ts.Month() != time.March {
		t.Fatalf("time = %v", ts)
	}
}
