package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, err := s.dbClient.GetSyncStatus(r.Context())
	if err != nil {
		s.sendError(w, "Failed to load sync status", http.StatusInternalServerError)
		return
	}

	out := map[string]interface{}{
		"lastSync":                   status.LastSync.UTC().Format(time.RFC3339),
		"lastScrobbleSubmissionTime": status.LastScrobbleSubmissionTime,
		"playEventsCount":            status.PlayEventsCount,
	}
	if status.LatestPlayEvent != nil {
		out["latestPlayEvent"] = status.LatestPlayEvent.UTC().Format(time.RFC3339)
	}
	if status.LastScrobbleSubmissionTime > 0 {
		out["scrobbleLagSeconds"] = time.Now().UTC().Unix() - status.LastScrobbleSubmissionTime
	}

	s.sendJSON(w, map[string]interface{}{"syncStatus": out})
}

func (s *Server) sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) sendError(w http.ResponseWriter, message string, code int) {
	w.WriteHeader(code)
	s.sendJSON(w, ChatResponse{Error: message})
}

func envInt(name string, defaultVal int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}

func envBool(name string, defaultVal bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return defaultVal
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
