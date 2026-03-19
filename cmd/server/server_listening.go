package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Server) handleRecentListeningSummary(ctx context.Context, lowerMsg string) (ChatResponse, bool) {
	start, end, _, ok := resolveListeningPeriod(lowerMsg)
	if !ok {
		return ChatResponse{}, false
	}
	if !(strings.Contains(lowerMsg, "listen") ||
		strings.Contains(lowerMsg, "listened") ||
		strings.Contains(lowerMsg, "songs") ||
		strings.Contains(lowerMsg, "tracks") ||
		strings.Contains(lowerMsg, "replay") ||
		strings.Contains(lowerMsg, "replaying")) {
		return ChatResponse{}, false
	}
	if strings.Contains(lowerMsg, "artist") {
		return ChatResponse{}, false
	}
	return s.buildRecentListeningSummaryResponse(ctx, start, end, lowerMsg)
}

func (s *Server) buildRecentListeningSummaryResponse(ctx context.Context, start, end time.Time, lowerMsg string) (ChatResponse, bool) {
	raw, err := executeTool(
		ctx,
		s.resolver,
		s.embeddingsURL,
		"recentListeningSummary",
		map[string]interface{}{
			"playedSince": start.Format(time.RFC3339),
			"playedUntil": end.Format(time.RFC3339),
			"trackLimit":  10,
			"artistLimit": 6,
		},
	)
	if err != nil {
		return ChatResponse{}, false
	}

	var parsed struct {
		Data struct {
			RecentListeningSummary struct {
				WindowStart  string `json:"windowStart"`
				WindowEnd    string `json:"windowEnd"`
				TracksHeard  int    `json:"tracksHeard"`
				TotalPlays   int    `json:"totalPlays"`
				ArtistsHeard int    `json:"artistsHeard"`
				TopTracks    []struct {
					Title      string `json:"title"`
					ArtistName string `json:"artistName"`
					PlayCount  int    `json:"playCount"`
				} `json:"topTracks"`
			} `json:"recentListeningSummary"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ChatResponse{}, false
	}

	summary := parsed.Data.RecentListeningSummary
	windowStart := humanizeSummaryTimestamp(summary.WindowStart)
	windowEnd := humanizeSummaryTimestamp(summary.WindowEnd)
	if summary.TotalPlays == 0 || len(summary.TopTracks) == 0 {
		return ChatResponse{Response: fmt.Sprintf("I don't see any listening activity in this window (%s to %s).", windowStart, windowEnd)}, true
	}

	top := summary.TopTracks
	if len(top) > 3 {
		top = top[:3]
	}
	items := make([]string, 0, len(top))
	for _, t := range top {
		items = append(items, fmt.Sprintf("%q by %s", t.Title, t.ArtistName))
	}

	resp := fmt.Sprintf(
		"From %s to %s, you played %d tracks across %d artists. Top songs: %s.",
		windowStart,
		windowEnd,
		summary.TotalPlays,
		summary.ArtistsHeard,
		strings.Join(items, ", "),
	)
	if strings.Contains(lowerMsg, "mood") || strings.Contains(lowerMsg, "vibe") || strings.Contains(lowerMsg, "feel") {
		mood := "balanced and exploratory"
		if summary.TotalPlays >= 20 && summary.ArtistsHeard <= 3 {
			mood = "focused and repeat-heavy"
		} else if summary.ArtistsHeard >= 8 {
			mood = "exploratory and variety-seeking"
		} else if summary.TotalPlays <= 6 {
			mood = "light and low-intensity"
		}
		resp += " Your listening pattern suggests a " + mood + " mood."
	}
	return ChatResponse{Response: resp}, true
}

func resolveListeningPeriodFromWindow(window string) (time.Time, time.Time, bool) {
	now := time.Now().In(time.Local)
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "last_month", "ambiguous_recent":
		return now.AddDate(0, -1, 0), now, true
	case "this_month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return start, now, true
	case "this_year":
		start := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, now.Location())
		return start, now, true
	default:
		return time.Time{}, time.Time{}, false
	}
}

func humanizeSummaryTimestamp(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	if ts, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return ts.Local().Format("Jan 2, 2006 3:04 PM")
	}
	return raw
}

func resolveListeningPeriod(q string) (time.Time, time.Time, string, bool) {
	now := time.Now().In(time.Local)
	switch {
	case strings.Contains(q, "today"), strings.Contains(q, "last day"), strings.Contains(q, "past day"):
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return start, now, "today", true
	case strings.Contains(q, "this week"), strings.Contains(q, "last week"), strings.Contains(q, "past week"):
		return now.AddDate(0, 0, -7), now, "in the last week", true
	case strings.Contains(q, "this month"):
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return start, now, fmt.Sprintf("since %s", start.Format("January 2, 2006")), true
	case strings.Contains(q, "last month"), strings.Contains(q, "past month"):
		return now.AddDate(0, -1, 0), now, "in the last month", true
	case strings.Contains(q, "this year"):
		start := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, now.Location())
		return start, now, fmt.Sprintf("since %s", start.Format("January 2, 2006")), true
	case strings.Contains(q, "last year"), strings.Contains(q, "past year"):
		return now.AddDate(-1, 0, 0), now, "in the last year", true
	case strings.Contains(q, "lately"), strings.Contains(q, "recently"), strings.Contains(q, "these days"), strings.Contains(q, "of late"):
		return now.AddDate(0, -1, 0), now, "in the last month", true
	default:
		return time.Time{}, time.Time{}, "", false
	}
}
