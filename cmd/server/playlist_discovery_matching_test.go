package main

import "testing"

func TestNormalizeSearchTermRemovesAccentsAndPunctuation(t *testing.T) {
	if got := normalizeSearchTerm("Röyksopp"); got != "royksopp" {
		t.Fatalf("normalizeSearchTerm(artist) = %q, want %q", got, "royksopp")
	}
	if got := normalizeSearchTerm("La Femme D'Argent"); got != "la femme d argent" {
		t.Fatalf("normalizeSearchTerm(title) = %q, want %q", got, "la femme d argent")
	}
	if got := normalizeSearchTerm("Hoppípolla"); got != "hoppipolla" {
		t.Fatalf("normalizeSearchTerm(accented title) = %q, want %q", got, "hoppipolla")
	}
}

func TestSearchTermVariantsStripMetadataAndFeaturedArtists(t *testing.T) {
	variants := searchTermVariants("Dayvan Cowboy (Remastered 2008) feat. Guest")
	want := []string{
		"dayvan cowboy remastered 2008 feat guest",
		"dayvan cowboy feat guest",
		"dayvan cowboy remastered 2008",
		"dayvan cowboy",
	}
	for _, item := range want {
		found := false
		for _, variant := range variants {
			if variant == item {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("searchTermVariants() missing %q in %#v", item, variants)
		}
	}
}

func TestSearchVariantSetExactMatchHandlesAccentDifferences(t *testing.T) {
	if !searchVariantSetExactMatch(searchTermVariants("Röyksopp"), searchTermVariants("Royksopp")) {
		t.Fatal("searchVariantSetExactMatch() = false, want true for accent-normalized artist")
	}
	if !searchVariantSetExactMatch(searchTermVariants("Hoppípolla"), searchTermVariants("Hoppipolla")) {
		t.Fatal("searchVariantSetExactMatch() = false, want true for accent-normalized title")
	}
}

func TestSearchVariantSetLooseMatchHandlesMetadataVariants(t *testing.T) {
	if !searchVariantSetLooseMatch(
		searchTermVariants("The Mark III"),
		searchTermVariants("The Mark III (Single Edit)"),
	) {
		t.Fatal("searchVariantSetLooseMatch() = false, want true for metadata suffix")
	}
	if searchVariantSetLooseMatch(
		searchTermVariants("Soon It Will Be Cold Enough"),
		searchTermVariants("With Rainy Eyes"),
	) {
		t.Fatal("searchVariantSetLooseMatch() = true, want false for unrelated title")
	}
}
