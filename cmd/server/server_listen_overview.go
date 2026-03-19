package main

import (
	"net/http"
	"os"
)

func (s *Server) handleListenOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var (
		similarityHealth interface{}
		syncStatus       interface{}
		tasteProfile     interface{}
		contextValue     interface{}
	)

	if s.similarity != nil {
		similarityHealth = s.similarity.Health(r.Context())
		if currentContext, err := s.similarity.GetListeningContext(r.Context()); err == nil {
			contextValue = currentContext
		}
	}
	if s.dbClient != nil {
		if status, err := s.dbClient.GetSyncStatus(r.Context()); err == nil {
			syncStatus = status
		}
		if summary, err := s.dbClient.GetTasteProfileSummary(r.Context()); err == nil {
			tasteProfile = summary
		}
	}

	s.sendJSON(w, map[string]interface{}{
		"listenOverview": map[string]interface{}{
			"navidromeConfigured": os.Getenv("NAVIDROME_DB_PATH") != "",
			"similarity":          similarityHealth,
			"syncStatus":          syncStatus,
			"tasteProfile":        tasteProfile,
			"context":             contextValue,
		},
	})
}
