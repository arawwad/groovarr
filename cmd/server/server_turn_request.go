package main

import (
	"encoding/json"
	"strings"
)

type serverTurnRequest struct {
	Intent              string                    `json:"intent"`
	SubIntent           string                    `json:"subIntent,omitempty"`
	FollowupMode        string                    `json:"followupMode"`
	QueryScope          string                    `json:"queryScope"`
	TimeWindow          string                    `json:"timeWindow"`
	Confidence          string                    `json:"confidence"`
	LibraryOnly         *bool                     `json:"libraryOnly,omitempty"`
	NeedsClarification  bool                      `json:"needsClarification"`
	ClarificationFocus  string                    `json:"clarificationFocus,omitempty"`
	ClarificationPrompt string                    `json:"clarificationPrompt,omitempty"`
	StyleHints          []string                  `json:"styleHints,omitempty"`
	TargetName          string                    `json:"targetName,omitempty"`
	ArtistName          string                    `json:"artistName,omitempty"`
	TrackTitle          string                    `json:"trackTitle,omitempty"`
	PromptHint          string                    `json:"promptHint,omitempty"`
	Reference           serverTurnReference       `json:"reference"`
	Workflow            serverTurnWorkflow        `json:"workflow"`
	Session             serverTurnSessionSnapshot `json:"session"`
}

type serverTurnReference struct {
	Target          string `json:"target"`
	Qualifier       string `json:"qualifier,omitempty"`
	RequestedSet    string `json:"requestedSet,omitempty"`
	ResolvedSet     string `json:"resolvedSet,omitempty"`
	ResolvedSource  string `json:"resolvedSource,omitempty"`
	ResolvedItemKey string `json:"resolvedItemKey,omitempty"`
	ResolvedItemRef string `json:"resolvedItemRef,omitempty"`
	MissingContext  bool   `json:"missingContext,omitempty"`
	Ambiguous       bool   `json:"ambiguous,omitempty"`
}

type serverTurnWorkflow struct {
	Action               string `json:"action,omitempty"`
	SelectionMode        string `json:"selectionMode,omitempty"`
	SelectionValue       string `json:"selectionValue,omitempty"`
	CompareSelectionMode string `json:"compareSelectionMode,omitempty"`
	CompareSelectionValue string `json:"compareSelectionValue,omitempty"`
}

type serverTurnSessionSnapshot struct {
	HasCreativeAlbumSet    bool `json:"hasCreativeAlbumSet,omitempty"`
	HasSemanticAlbumSet    bool `json:"hasSemanticAlbumSet,omitempty"`
	HasDiscoveredAlbums    bool `json:"hasDiscoveredAlbums,omitempty"`
	HasCleanupCandidates   bool `json:"hasCleanupCandidates,omitempty"`
	HasBadlyRatedAlbums    bool `json:"hasBadlyRatedAlbums,omitempty"`
	HasRecentListening     bool `json:"hasRecentListening,omitempty"`
	HasPendingPlaylistPlan bool `json:"hasPendingPlaylistPlan,omitempty"`
	HasResolvedScene       bool `json:"hasResolvedScene,omitempty"`
	HasSongPath            bool `json:"hasSongPath,omitempty"`
	HasTrackCandidates     bool `json:"hasTrackCandidates,omitempty"`
	HasArtistCandidates    bool `json:"hasArtistCandidates,omitempty"`
}

func buildServerTurnRequest(resolved *resolvedTurnContext) serverTurnRequest {
	if resolved == nil {
		return serverTurnRequest{}
	}
	ref := resolved.resultReference()
	return serverTurnRequest{
		Intent:              strings.TrimSpace(resolved.Turn.Intent),
		SubIntent:           strings.TrimSpace(resolved.Turn.SubIntent),
		FollowupMode:        strings.TrimSpace(resolved.Turn.FollowupMode),
		QueryScope:          strings.TrimSpace(resolved.Turn.QueryScope),
		TimeWindow:          strings.TrimSpace(resolved.Turn.TimeWindow),
		Confidence:          strings.TrimSpace(resolved.Turn.Confidence),
		LibraryOnly:         resolved.Turn.LibraryOnly,
		NeedsClarification:  resolved.Turn.NeedsClarification,
		ClarificationFocus:  strings.TrimSpace(resolved.Turn.ClarificationFocus),
		ClarificationPrompt: strings.TrimSpace(resolved.Turn.ClarificationPrompt),
		StyleHints:          append([]string(nil), resolved.Turn.StyleHints...),
		TargetName:          strings.TrimSpace(resolved.Turn.TargetName),
		ArtistName:          strings.TrimSpace(resolved.Turn.ArtistName),
		TrackTitle:          strings.TrimSpace(resolved.Turn.TrackTitle),
		PromptHint:          strings.TrimSpace(resolved.Turn.PromptHint),
		Reference: serverTurnReference{
			Target:          strings.TrimSpace(resolved.Turn.ReferenceTarget),
			Qualifier:       strings.TrimSpace(resolved.Turn.ReferenceQualifier),
			RequestedSet:    strings.TrimSpace(ref.SetKind),
			ResolvedSet:     strings.TrimSpace(ref.ResolvedSetKind),
			ResolvedSource:  strings.TrimSpace(ref.ResolvedSource),
			ResolvedItemKey: strings.TrimSpace(ref.ResolvedItemKey),
			ResolvedItemRef: strings.TrimSpace(ref.ResolvedItemRef),
			MissingContext:  resolved.MissingReferenceContext,
			Ambiguous:       resolved.AmbiguousReference,
		},
		Workflow: serverTurnWorkflow{
			Action:                strings.TrimSpace(ref.Action),
			SelectionMode:         strings.TrimSpace(ref.Selection.Mode),
			SelectionValue:        strings.TrimSpace(ref.Selection.Value),
			CompareSelectionMode:  strings.TrimSpace(resolved.Turn.CompareSelectionMode),
			CompareSelectionValue: strings.TrimSpace(resolved.Turn.CompareSelectionValue),
		},
		Session: serverTurnSessionSnapshot{
			HasCreativeAlbumSet:    resolved.HasCreativeAlbumSet,
			HasSemanticAlbumSet:    resolved.HasSemanticAlbumSet,
			HasDiscoveredAlbums:    resolved.HasDiscoveredAlbums,
			HasCleanupCandidates:   resolved.HasCleanupCandidates,
			HasBadlyRatedAlbums:    resolved.HasBadlyRatedAlbums,
			HasRecentListening:     resolved.HasRecentListening,
			HasPendingPlaylistPlan: resolved.HasPendingPlaylistPlan,
			HasResolvedScene:       resolved.HasResolvedScene,
			HasSongPath:            resolved.HasSongPath,
			HasTrackCandidates:     resolved.HasTrackCandidates,
			HasArtistCandidates:    resolved.HasArtistCandidates,
		},
	}
}

func renderServerTurnRequest(resolved *resolvedTurnContext) string {
	if resolved == nil {
		return "none"
	}
	payload, err := json.Marshal(buildServerTurnRequest(resolved))
	if err != nil {
		return "none"
	}
	return string(payload)
}
