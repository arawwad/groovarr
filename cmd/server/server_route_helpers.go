package main

import (
	"fmt"
	"strings"
)

func renderRouteBulletList(prefix string, items []string, limit int) string {
	if len(items) == 0 {
		return prefix + "."
	}
	if limit <= 0 {
		limit = 8
	}
	visible := items
	remaining := 0
	if len(items) > limit {
		visible = items[:limit]
		remaining = len(items) - limit
	}
	lines := make([]string, 0, len(visible)+2)
	lines = append(lines, prefix+":")
	for _, item := range visible {
		lines = append(lines, "- "+item)
	}
	if remaining > 0 {
		lines = append(lines, fmt.Sprintf("- ...and %d more.", remaining))
	}
	return strings.Join(lines, "\n")
}

func containsLibraryOwnershipCue(lowerMsg string) bool {
	cues := []string{
		"my library",
		"in my library",
		"from my library",
		"i own",
		"owned",
		"my albums",
		"my records",
		"my collection",
		"my shelves",
		"from what i have",
		"from what i've got",
		"from what i already have",
		"already have",
	}
	for _, cue := range cues {
		if strings.Contains(lowerMsg, cue) {
			return true
		}
	}
	return false
}

func isCreativeThreeAlbumPrompt(lowerMsg string) bool {
	return strings.Contains(lowerMsg, "focus") &&
		strings.Contains(lowerMsg, "walking") &&
		(strings.Contains(lowerMsg, "late-night headphones") ||
			(strings.Contains(lowerMsg, "late-night") && strings.Contains(lowerMsg, "headphones")) ||
			(strings.Contains(lowerMsg, "late night") && strings.Contains(lowerMsg, "headphones")))
}

func isSemanticLibraryPrompt(lowerMsg string) bool {
	if lowerMsg == "" {
		return false
	}
	if isCreativeThreeAlbumPrompt(lowerMsg) {
		return true
	}
	cues := []string{
		"vibe",
		"mood",
		"feels like",
		"feel like",
		"sounds like",
		"similar to",
		"weirdest corner",
		"weird corner",
		"nocturnal",
		"atmospheric",
		"dreamy",
		"immersive",
		"spacious",
		"rainy",
		"moody",
		"melancholic",
		"late-night",
		"late night",
	}
	for _, cue := range cues {
		if strings.Contains(lowerMsg, cue) {
			return true
		}
	}
	return false
}
