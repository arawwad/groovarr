package main

import (
	"encoding/json"
	"sort"
	"strings"
)

type orchestrationDecision struct {
	NextStage           string `json:"nextStage"`
	DeterministicMode   string `json:"deterministicMode,omitempty"`
	ClarificationPrompt string `json:"clarificationPrompt,omitempty"`
	Reason              string `json:"reason,omitempty"`
	Confidence          string `json:"confidence"`
}

type resultSetCapability struct {
	SetKind    string   `json:"setKind"`
	Operations []string `json:"operations,omitempty"`
	Selectors  []string `json:"selectors,omitempty"`
}

type resultSetResolverRequest struct {
	Turn         serverTurnRequest     `json:"turn"`
	Capabilities []resultSetCapability `json:"capabilities,omitempty"`
}

type resultSetResolverDecision struct {
	SetKind             string `json:"setKind,omitempty"`
	ItemKey             string `json:"itemKey,omitempty"`
	Operation           string `json:"operation,omitempty"`
	SelectionMode       string `json:"selectionMode,omitempty"`
	SelectionValue      string `json:"selectionValue,omitempty"`
	CompareSelectionMode  string `json:"compareSelectionMode,omitempty"`
	CompareSelectionValue string `json:"compareSelectionValue,omitempty"`
	NeedsClarification  bool   `json:"needsClarification,omitempty"`
	ClarificationPrompt string `json:"clarificationPrompt,omitempty"`
	Reason              string `json:"reason,omitempty"`
	Confidence          string `json:"confidence,omitempty"`
}

type serverExecutionRequest struct {
	Domain         string `json:"domain"`
	SetKind        string `json:"setKind,omitempty"`
	Operation      string `json:"operation,omitempty"`
	SelectionMode  string `json:"selectionMode,omitempty"`
	SelectionValue string `json:"selectionValue,omitempty"`
	CompareSelectionMode  string `json:"compareSelectionMode,omitempty"`
	CompareSelectionValue string `json:"compareSelectionValue,omitempty"`
	ItemKey        string `json:"itemKey,omitempty"`
	TargetName     string `json:"targetName,omitempty"`
	ArtistName     string `json:"artistName,omitempty"`
	TrackTitle     string `json:"trackTitle,omitempty"`
	PromptHint     string `json:"promptHint,omitempty"`
	TimeWindow     string `json:"timeWindow,omitempty"`
}

func buildResultSetResolverRequest(resolved *resolvedTurnContext) resultSetResolverRequest {
	return resultSetResolverRequest{
		Turn:         buildServerTurnRequest(resolved),
		Capabilities: currentResultSetCapabilities(),
	}
}

func currentResultSetCapabilities() []resultSetCapability {
	capabilities := append([]resultSetCapability{}, currentAdapterResultSetCapabilities()...)
	capabilities = append(capabilities,
		creativeResultSetCapability("creative_albums"),
		creativeResultSetCapability("semantic_albums"),
		playlistCandidateResultSetCapability(),
		trackCandidateResultSetCapability(),
		artistCandidateResultSetCapability(),
	)
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].SetKind < capabilities[j].SetKind
	})
	return capabilities
}

func buildServerExecutionRequest(resolved *resolvedTurnContext, decision resultSetResolverDecision) serverExecutionRequest {
	request := serverExecutionRequest{
		Domain:         strings.TrimSpace(resolved.Turn.Intent),
		SetKind:        strings.TrimSpace(decision.SetKind),
		Operation:      strings.TrimSpace(decision.Operation),
		SelectionMode:  strings.TrimSpace(decision.SelectionMode),
		SelectionValue: strings.TrimSpace(decision.SelectionValue),
		CompareSelectionMode:  strings.TrimSpace(decision.CompareSelectionMode),
		CompareSelectionValue: strings.TrimSpace(decision.CompareSelectionValue),
		ItemKey:        strings.TrimSpace(decision.ItemKey),
		TargetName:     strings.TrimSpace(resolved.Turn.TargetName),
		ArtistName:     strings.TrimSpace(resolved.Turn.ArtistName),
		TrackTitle:     strings.TrimSpace(resolved.Turn.TrackTitle),
		PromptHint:     strings.TrimSpace(resolved.Turn.PromptHint),
		TimeWindow:     strings.TrimSpace(resolved.Turn.TimeWindow),
	}
	if request.Domain == "" {
		request.Domain = "other"
	}
	if request.SetKind == "" {
		request.SetKind = strings.TrimSpace(resolved.ResolvedReferenceKind)
	}
	if request.Operation == "" {
		request.Operation = strings.TrimSpace(resolved.Turn.ResultAction)
	}
	if request.SelectionMode == "" {
		request.SelectionMode = strings.TrimSpace(resolved.Turn.SelectionMode)
	}
	if request.SelectionValue == "" {
		request.SelectionValue = strings.TrimSpace(resolved.Turn.SelectionValue)
	}
	if request.CompareSelectionMode == "" {
		request.CompareSelectionMode = strings.TrimSpace(resolved.Turn.CompareSelectionMode)
	}
	if request.CompareSelectionValue == "" {
		request.CompareSelectionValue = strings.TrimSpace(resolved.Turn.CompareSelectionValue)
	}
	if request.ItemKey == "" {
		request.ItemKey = strings.TrimSpace(resolved.ResolvedItemKey)
	}
	return request
}

func renderResultSetResolverRequest(resolved *resolvedTurnContext) string {
	payload, err := json.Marshal(buildResultSetResolverRequest(resolved))
	if err != nil {
		return "none"
	}
	return string(payload)
}

func renderServerExecutionRequest(request serverExecutionRequest) string {
	payload, err := json.Marshal(request)
	if err != nil {
		return "none"
	}
	return string(payload)
}
