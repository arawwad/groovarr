package db

import "testing"

func TestNormalizeSearchKeyNormalizesUnicodePunctuation(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "Heart-Shaped Box", want: "heart shaped box"},
		{in: "Heart‐Shaped Box", want: "heart shaped box"},
		{in: "The Dark Side of the Moon", want: "the dark side of the moon"},
		{in: "Björk", want: "bjork"},
	}

	for _, tt := range tests {
		if got := normalizeSearchKey(tt.in); got != tt.want {
			t.Fatalf("normalizeSearchKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
