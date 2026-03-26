package main

import (
	"encoding/json"
	"strings"
)

type serverTurnRequest struct {
	Intent              string                    `json:"intent"`
	SubIntent           string                    `json:"subIntent,omitempty"`
	ConversationOp      string                    `json:"conversationOp,omitempty"`
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
	Conversation        serverTurnConversation    `json:"conversation,omitempty"`
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
	Action                string `json:"action,omitempty"`
	SelectionMode         string `json:"selectionMode,omitempty"`
	SelectionValue        string `json:"selectionValue,omitempty"`
	CompareSelectionMode  string `json:"compareSelectionMode,omitempty"`
	CompareSelectionValue string `json:"compareSelectionValue,omitempty"`
}

type serverTurnConversation struct {
	ObjectType   string `json:"objectType,omitempty"`
	ObjectKind   string `json:"objectKind,omitempty"`
	ObjectStatus string `json:"objectStatus,omitempty"`
	ObjectIntent string `json:"objectIntent,omitempty"`
	ObjectTarget string `json:"objectTarget,omitempty"`
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
	return buildServerTurnRequestFromTurn(turnFromResolved(resolved))
}

func buildServerTurnRequestFromTurn(turn *Turn) serverTurnRequest {
	if turn == nil {
		return serverTurnRequest{}
	}
	return serverTurnRequest{
		Intent:              strings.TrimSpace(turn.Normalized.Intent),
		SubIntent:           strings.TrimSpace(turn.Normalized.SubIntent),
		ConversationOp:      strings.TrimSpace(turn.Normalized.ConversationOp),
		FollowupMode:        strings.TrimSpace(turn.Normalized.FollowupMode),
		QueryScope:          strings.TrimSpace(turn.Normalized.QueryScope),
		TimeWindow:          strings.TrimSpace(turn.Normalized.TimeWindow),
		Confidence:          strings.TrimSpace(turn.Normalized.Confidence),
		LibraryOnly:         turn.Normalized.LibraryOnly,
		NeedsClarification:  turn.Normalized.NeedsClarification,
		ClarificationFocus:  strings.TrimSpace(turn.Normalized.ClarificationFocus),
		ClarificationPrompt: strings.TrimSpace(turn.Normalized.ClarificationPrompt),
		StyleHints:          append([]string(nil), turn.Normalized.StyleHints...),
		TargetName:          strings.TrimSpace(turn.Normalized.TargetName),
		ArtistName:          strings.TrimSpace(turn.Normalized.ArtistName),
		TrackTitle:          strings.TrimSpace(turn.Normalized.TrackTitle),
		PromptHint:          strings.TrimSpace(turn.Normalized.PromptHint),
		Reference: serverTurnReference{
			Target:          strings.TrimSpace(turn.Reference.Target),
			Qualifier:       strings.TrimSpace(turn.Reference.Qualifier),
			RequestedSet:    strings.TrimSpace(turn.Reference.RequestedSet),
			ResolvedSet:     strings.TrimSpace(turn.Reference.ResolvedSet),
			ResolvedSource:  strings.TrimSpace(turn.Reference.ResolvedSource),
			ResolvedItemKey: strings.TrimSpace(turn.Reference.ResolvedItemKey),
			ResolvedItemRef: strings.TrimSpace(turn.Reference.ResolvedItemRef),
			MissingContext:  turn.Reference.MissingContext,
			Ambiguous:       turn.Reference.Ambiguous,
		},
		Workflow: serverTurnWorkflow{
			Action:                strings.TrimSpace(turn.Normalized.ResultAction),
			SelectionMode:         strings.TrimSpace(turn.Normalized.SelectionMode),
			SelectionValue:        strings.TrimSpace(turn.Normalized.SelectionValue),
			CompareSelectionMode:  strings.TrimSpace(turn.Normalized.CompareSelectionMode),
			CompareSelectionValue: strings.TrimSpace(turn.Normalized.CompareSelectionValue),
		},
		Conversation: serverTurnConversation{
			ObjectType:   strings.TrimSpace(turn.Reference.ObjectType),
			ObjectKind:   strings.TrimSpace(turn.Reference.ObjectKind),
			ObjectStatus: strings.TrimSpace(turn.Reference.ObjectStatus),
			ObjectIntent: strings.TrimSpace(turn.Reference.ObjectIntent),
			ObjectTarget: strings.TrimSpace(turn.Reference.ObjectTarget),
		},
		Session: serverTurnSessionSnapshot{
			HasCreativeAlbumSet:    turn.Reference.HasCreativeAlbumSet,
			HasSemanticAlbumSet:    turn.Reference.HasSemanticAlbumSet,
			HasDiscoveredAlbums:    turn.Reference.HasDiscoveredAlbums,
			HasCleanupCandidates:   turn.Reference.HasCleanupCandidates,
			HasBadlyRatedAlbums:    turn.Reference.HasBadlyRatedAlbums,
			HasRecentListening:     turn.Reference.HasRecentListening,
			HasPendingPlaylistPlan: turn.Reference.HasPendingPlaylistPlan,
			HasResolvedScene:       turn.Reference.HasResolvedScene,
			HasSongPath:            turn.Reference.HasSongPath,
			HasTrackCandidates:     turn.Reference.HasTrackCandidates,
			HasArtistCandidates:    turn.Reference.HasArtistCandidates,
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
