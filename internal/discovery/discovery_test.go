package discovery

import "testing"

func TestSelectCandidatesSupportsCommaSeparatedSelections(t *testing.T) {
	candidates := []Candidate{
		{Rank: 1, ArtistName: "Pink Floyd", AlbumTitle: "The Dark Side of the Moon"},
		{Rank: 2, ArtistName: "Pink Floyd", AlbumTitle: "Wish You Were Here"},
		{Rank: 3, ArtistName: "Pink Floyd", AlbumTitle: "The Wall"},
	}

	selected, err := SelectCandidates(candidates, "The Dark Side of the Moon, Wish You Were Here, The Wall")
	if err != nil {
		t.Fatalf("SelectCandidates returned error: %v", err)
	}
	if len(selected) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(selected))
	}
	for i, candidate := range selected {
		if candidate.Rank != i+1 {
			t.Fatalf("expected rank %d at index %d, got %d", i+1, i, candidate.Rank)
		}
	}
}

func TestSelectCandidatesSupportsAlbumArtistSelections(t *testing.T) {
	candidates := []Candidate{
		{Rank: 1, ArtistName: "Pink Floyd", AlbumTitle: "The Dark Side of the Moon"},
		{Rank: 2, ArtistName: "Radiohead", AlbumTitle: "Kid A"},
	}

	selected, err := SelectCandidates(candidates, "Kid A by Radiohead")
	if err != nil {
		t.Fatalf("SelectCandidates returned error: %v", err)
	}
	if len(selected) != 1 {
		t.Fatalf("expected 1 match, got %d", len(selected))
	}
	if selected[0].Rank != 2 {
		t.Fatalf("expected rank 2 match, got %d", selected[0].Rank)
	}
}

func TestInferArtistFocus(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{query: "best albums by Muse", want: "Muse"},
		{query: "What are best albums for Muse?", want: "Muse"},
		{query: "top albums Everything Everything", want: "Everything Everything"},
		{query: "best 3 Pink Floyd albums", want: "Pink Floyd"},
		{query: "three records for a rainy late-night walk", want: ""},
	}

	for _, tc := range tests {
		got := InferArtistFocus(tc.query)
		if got != tc.want {
			t.Fatalf("InferArtistFocus(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestBuildRequestClampsLimitAndInfersArtist(t *testing.T) {
	request, err := BuildRequest("best albums by Muse", 99)
	if err != nil {
		t.Fatalf("BuildRequest returned error: %v", err)
	}
	if request.Limit != 8 {
		t.Fatalf("expected limit clamp to 8, got %d", request.Limit)
	}
	if request.RequestCount != 10 {
		t.Fatalf("expected requestCount 10, got %d", request.RequestCount)
	}
	if request.ArtistHint != "Muse" {
		t.Fatalf("expected artist hint Muse, got %q", request.ArtistHint)
	}
}

func TestBuildPromptsIncludesArtistFocus(t *testing.T) {
	request := Request{
		Query:        "best albums by Muse",
		ArtistHint:   "Muse",
		Limit:        5,
		RequestCount: 7,
	}
	_, userPrompt := BuildPrompts(request)
	if userPrompt != "Find up to 7 high-confidence albums for this request: best albums by Muse\nArtist focus: Muse" {
		t.Fatalf("unexpected user prompt: %q", userPrompt)
	}
}

func TestNormalizeTitleFoldsDiacritics(t *testing.T) {
	got := NormalizeTitle("Björk")
	if got != "bjork" {
		t.Fatalf("NormalizeTitle(%q) = %q, want %q", "Björk", got, "bjork")
	}
	if TitleSimilarity(NormalizeTitle("Bjork"), NormalizeTitle("Björk")) != 1 {
		t.Fatalf("expected Bjork and Björk to normalize to identical titles")
	}
}

func TestRankFiltersAndRanks(t *testing.T) {
	request := Request{
		Query:      "best albums by Muse",
		ArtistHint: "Muse",
		Limit:      2,
	}
	ranked := Rank(request, []Candidate{
		{ArtistName: "Muse", AlbumTitle: "Absolution", Year: 2003, Reason: "classic and essential"},
		{ArtistName: "Muse", AlbumTitle: "Absolution", Year: 2003, Reason: "duplicate"},
		{ArtistName: "Muse", AlbumTitle: "Live at Rome", Year: 2013, Reason: "live favorite"},
		{ArtistName: "Radiohead", AlbumTitle: "OK Computer", Year: 1997, Reason: "classic"},
		{ArtistName: "Muse", AlbumTitle: "Origin of Symmetry", Year: 2001, Reason: "iconic masterpiece"},
	})
	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked albums, got %d", len(ranked))
	}
	if ranked[0].AlbumTitle != "Origin of Symmetry" {
		t.Fatalf("expected top ranked album to be Origin of Symmetry, got %q", ranked[0].AlbumTitle)
	}
	if ranked[1].AlbumTitle != "Absolution" {
		t.Fatalf("expected second ranked album to be Absolution, got %q", ranked[1].AlbumTitle)
	}
	if ranked[0].Rank != 1 || ranked[1].Rank != 2 {
		t.Fatalf("expected ranks 1 and 2, got %d and %d", ranked[0].Rank, ranked[1].Rank)
	}
}

func TestRankRejectsPromptEchoCandidatesForMoodQueries(t *testing.T) {
	request := Request{
		Query: "three records for a rainy late-night walk",
		Limit: 3,
	}
	ranked := Rank(request, []Candidate{
		{ArtistName: "a rainy late-night walk", AlbumTitle: "Three Records for a Rainy Late-Night Walk", Year: 2023},
		{ArtistName: "Moby", AlbumTitle: "Play", Year: 1999, Reason: "moody nocturnal electronics"},
	})
	if len(ranked) != 1 {
		t.Fatalf("expected only the real candidate to remain, got %d (%#v)", len(ranked), ranked)
	}
	if ranked[0].ArtistName != "Moby" || ranked[0].AlbumTitle != "Play" {
		t.Fatalf("unexpected surviving candidate: %#v", ranked[0])
	}
}

func TestRankRejectsSeedEchoButKeepsRealSeededAlternatives(t *testing.T) {
	request := Request{
		Query: "records like Talk Talk's Laughing Stock but warmer and more spacious",
		Limit: 5,
	}
	ranked := Rank(request, []Candidate{
		{ArtistName: "Talk Talk", AlbumTitle: "Laughing Stock", Year: 1991, Reason: "seed reference"},
		{ArtistName: "Talk Talk", AlbumTitle: "The Colour of Spring", Year: 1986, Reason: "warm and spacious"},
	})
	if len(ranked) != 1 {
		t.Fatalf("expected only the non-echo alternative to remain, got %d (%#v)", len(ranked), ranked)
	}
	if ranked[0].AlbumTitle != "The Colour of Spring" {
		t.Fatalf("expected surviving candidate to be The Colour of Spring, got %#v", ranked[0])
	}
}
