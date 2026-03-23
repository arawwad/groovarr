package lidarr

import "testing"

func TestSelectExistingArtist(t *testing.T) {
	tests := []struct {
		name     string
		search   string
		existing []Artist
		results  []ArtistSearchResult
		wantID   int
	}{
		{
			name:   "exact match wins",
			search: "M83",
			existing: []Artist{
				{ID: 7, ArtistName: "M83", ForeignArtistID: "mbid-m83"},
			},
			wantID: 7,
		},
		{
			name:   "lookup foreign artist id matches existing artist",
			search: "The Cure",
			existing: []Artist{
				{ID: 11, ArtistName: "The Cure", ForeignArtistID: "mbid-cure"},
			},
			results: []ArtistSearchResult{
				{ArtistName: "The Cure", ForeignArtistID: "mbid-cure", Genres: []string{"rock"}},
			},
			wantID: 11,
		},
		{
			name:   "no match returns nil",
			search: "Burial",
			existing: []Artist{
				{ID: 21, ArtistName: "Boards of Canada", ForeignArtistID: "mbid-boc"},
			},
			results: []ArtistSearchResult{
				{ArtistName: "Burial", ForeignArtistID: "mbid-burial", Genres: []string{"electronic"}},
			},
			wantID: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectExistingArtist(tc.search, tc.existing, tc.results)
			if tc.wantID == 0 {
				if got != nil {
					t.Fatalf("SelectExistingArtist(%q) = %+v, want nil", tc.search, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("SelectExistingArtist(%q) = nil, want id %d", tc.search, tc.wantID)
			}
			if got.ID != tc.wantID {
				t.Fatalf("SelectExistingArtist(%q) id = %d, want %d", tc.search, got.ID, tc.wantID)
			}
		})
	}
}

func TestSelectExistingArtistMatchesDiacritics(t *testing.T) {
	got := SelectExistingArtist("Bjork", []Artist{
		{ID: 9, ArtistName: "Björk", ForeignArtistID: "mbid-bjork"},
	}, nil)
	if got == nil {
		t.Fatalf("SelectExistingArtist(%q) = nil, want match", "Bjork")
	}
	if got.ID != 9 {
		t.Fatalf("SelectExistingArtist(%q) id = %d, want %d", "Bjork", got.ID, 9)
	}
}
