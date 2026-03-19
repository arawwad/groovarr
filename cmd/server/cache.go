package main

import "sync"

var recentListeningSummaryCache = struct {
	mu      sync.RWMutex
	entries map[string]cachedToolResult
}{
	entries: make(map[string]cachedToolResult),
}
