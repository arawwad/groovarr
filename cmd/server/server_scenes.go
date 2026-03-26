package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"groovarr/internal/agent"
)

func (s *Server) resolveStructuredSceneSelectionOutcome(ctx context.Context, resolved *resolvedTurnContext) (resultSetActionResult, bool) {
	ref := resolved.resultReference()
	if resolved == nil || ref.effectiveSetKind() != "scene_candidates" || ref.Action != "select_candidate" {
		return resultSetActionResult{}, false
	}
	sessionID := chatSessionIDFromContext(ctx)
	return handleResultSetAction(ctx, s, sessionID, ref)
}

func (s *Server) handleStructuredSceneOverview(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	if resolved == nil || strings.TrimSpace(resolved.Turn.SubIntent) != "scene_overview" {
		return "", false
	}
	raw, err := executeTool(ctx, s.resolver, s.embeddingsURL, "clusterScenes", map[string]interface{}{
		"queryText":    strings.TrimSpace(resolved.Turn.PromptHint),
		"limit":        6,
		"sampleTracks": 3,
	})
	if err != nil {
		return "", false
	}
	return renderStructuredSceneOverview(raw)
}

func (s *Server) handleStructuredSceneOverviewTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredSceneOverview(ctx, turnToResolvedTurnContext(turn))
}

func renderStructuredSceneOverview(raw string) (string, bool) {
	var payload struct {
		Data struct {
			ClusterScenes struct {
				Message string `json:"message"`
				Scenes  []struct {
					Name         string `json:"name"`
					Subtitle     string `json:"subtitle"`
					SongCount    int    `json:"songCount"`
					SampleTracks []struct {
						Title      string `json:"title"`
						ArtistName string `json:"artistName"`
					} `json:"sampleTracks"`
				} `json:"scenes"`
			} `json:"clusterScenes"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", false
	}
	scenes := payload.Data.ClusterScenes.Scenes
	if len(scenes) == 0 {
		message := strings.TrimSpace(payload.Data.ClusterScenes.Message)
		if message == "" {
			message = "I couldn't find any sonic scenes in your library yet."
		}
		return message, true
	}
	lines := make([]string, 0, len(scenes)+1)
	lines = append(lines, fmt.Sprintf("I split your library into %d sound neighborhoods:", len(scenes)))
	for _, scene := range scenes {
		label := strings.TrimSpace(scene.Name)
		if subtitle := strings.TrimSpace(scene.Subtitle); subtitle != "" {
			label += " [" + subtitle + "]"
		}
		label += fmt.Sprintf(" (%d tracks)", scene.SongCount)
		sample := make([]string, 0, len(scene.SampleTracks))
		for _, track := range scene.SampleTracks {
			if len(sample) >= 2 {
				break
			}
			title := strings.TrimSpace(track.Title)
			if artist := strings.TrimSpace(track.ArtistName); artist != "" {
				title += " by " + artist
			}
			if title != "" {
				sample = append(sample, title)
			}
		}
		if len(sample) > 0 {
			label += " sample: " + strings.Join(sample, "; ")
		}
		lines = append(lines, "- "+label)
	}
	lines = append(lines, "You can ask me to open one, pull tracks from it, or expand it.")
	return strings.Join(lines, "\n"), true
}

func renderStructuredSceneSelection(outcome resultSetActionResult) (string, bool) {
	switch outcome.Kind {
	case "scene_select":
		selected := outcome.Scene
		if selected == nil {
			return "", false
		}
		response := fmt.Sprintf("Using %s", strings.TrimSpace(selected.Name))
		if subtitle := strings.TrimSpace(selected.Subtitle); subtitle != "" {
			response += " (" + subtitle + ")"
		}
		response += fmt.Sprintf(" with %d tracks.", selected.SongCount)
		if sample := formatSceneSampleTracks(selected.SampleTracks); sample != "" {
			response += " Sample tracks: " + sample + "."
		}
		response += " You can ask for tracks from this scene, expand it, or discover albums from it."
		return response, true
	default:
		return "", false
	}
}

func (s *Server) handleStructuredSceneSelection(ctx context.Context, resolved *resolvedTurnContext) (string, bool) {
	outcome, ok := s.resolveStructuredSceneSelectionOutcome(ctx, resolved)
	if !ok {
		return "", false
	}
	return renderStructuredSceneSelection(outcome)
}

func (s *Server) handleStructuredSceneSelectionTurn(ctx context.Context, turn *Turn) (string, bool) {
	return s.handleStructuredSceneSelection(ctx, turnToResolvedTurnContext(turn))
}

func sceneExecutionHandlers() []serverExecutionHandler {
	return []serverExecutionHandler{
		{
			name: "scene_select",
			canHandle: func(turn *Turn) bool {
				request := executionRequestFromTurn(turn)
				return strings.TrimSpace(request.SetKind) == "scene_candidates" &&
					strings.TrimSpace(request.Operation) == "select_candidate"
			},
			executeWithTurn: func(ctx context.Context, s *Server, _ []agent.Message, turn *Turn) (ChatResponse, bool) {
				outcome, ok := s.resolveStructuredSceneSelectionOutcome(ctx, turnToResolvedTurnContext(turn))
				if !ok {
					return ChatResponse{}, false
				}
				if resp, ok := renderStructuredSceneSelection(outcome); ok {
					return ChatResponse{Response: resp}, true
				}
				return ChatResponse{}, false
			},
		},
	}
}

func resolveSceneCandidateFromReference(ref resolvedResultReference, candidates []sceneSessionItem) (*sceneSessionItem, bool) {
	if len(candidates) == 0 {
		return nil, false
	}
	switch ref.Selection.Mode {
	case "all", "none", "":
		return nil, false
	case "ordinal":
		value := strings.ToLower(strings.TrimSpace(ref.Selection.Value))
		switch value {
		case "1", "1st", "first":
			resolved := candidates[0]
			return &resolved, true
		case "2", "2nd", "second":
			if len(candidates) >= 2 {
				resolved := candidates[1]
				return &resolved, true
			}
		case "3", "3rd", "third":
			if len(candidates) >= 3 {
				resolved := candidates[2]
				return &resolved, true
			}
		case "last":
			resolved := candidates[len(candidates)-1]
			return &resolved, true
		}
	case "count_match":
		n, err := strconv.Atoi(strings.TrimSpace(ref.Selection.Value))
		if err != nil || n <= 0 {
			return nil, false
		}
		var matched *sceneSessionItem
		for _, candidate := range candidates {
			if candidate.SongCount != n {
				continue
			}
			if matched != nil {
				return nil, false
			}
			resolved := candidate
			matched = &resolved
		}
		if matched != nil {
			return matched, true
		}
	case "explicit_names":
		needle := normalizeReferenceText(ref.Selection.Value)
		if needle == "" {
			return nil, false
		}
		for _, candidate := range candidates {
			for _, field := range []string{candidate.Key, candidate.Name, candidate.Subtitle} {
				fieldKey := normalizeReferenceText(field)
				if fieldKey != "" && strings.Contains(fieldKey, needle) {
					resolved := candidate
					return &resolved, true
				}
			}
		}
	}
	return nil, false
}
