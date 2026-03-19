package main

import (
	"strings"
)

func recentRequestLooksLikeBadlyRatedAlbums(memory chatSessionMemory) bool {
	requests := append([]string(nil), memory.RecentUserRequests...)
	if text := strings.TrimSpace(memory.ActiveRequest); text != "" {
		requests = append(requests, text)
	}
	for index := len(requests) - 1; index >= 0; index-- {
		if isBadlyRatedAlbumsRequest(requests[index]) {
			return true
		}
	}
	return false
}

func isBadlyRatedAlbumsRequest(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "badly rated") ||
		strings.Contains(lower, "disliked album") ||
		strings.Contains(lower, "disliked albums") ||
		(strings.Contains(lower, "album") && strings.Contains(lower, "1-star")) ||
		(strings.Contains(lower, "album") && strings.Contains(lower, "2-star"))
}
