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
	SetKind               string `json:"setKind,omitempty"`
	ItemKey               string `json:"itemKey,omitempty"`
	Operation             string `json:"operation,omitempty"`
	SelectionMode         string `json:"selectionMode,omitempty"`
	SelectionValue        string `json:"selectionValue,omitempty"`
	CompareSelectionMode  string `json:"compareSelectionMode,omitempty"`
	CompareSelectionValue string `json:"compareSelectionValue,omitempty"`
	NeedsClarification    bool   `json:"needsClarification,omitempty"`
	ClarificationPrompt   string `json:"clarificationPrompt,omitempty"`
	Reason                string `json:"reason,omitempty"`
	Confidence            string `json:"confidence,omitempty"`
}

type serverExecutionRequest struct {
	Domain                string `json:"domain"`
	SetKind               string `json:"setKind,omitempty"`
	Operation             string `json:"operation,omitempty"`
	SelectionMode         string `json:"selectionMode,omitempty"`
	SelectionValue        string `json:"selectionValue,omitempty"`
	CompareSelectionMode  string `json:"compareSelectionMode,omitempty"`
	CompareSelectionValue string `json:"compareSelectionValue,omitempty"`
	ItemKey               string `json:"itemKey,omitempty"`
	TargetName            string `json:"targetName,omitempty"`
	ArtistName            string `json:"artistName,omitempty"`
	TrackTitle            string `json:"trackTitle,omitempty"`
	PromptHint            string `json:"promptHint,omitempty"`
	TimeWindow            string `json:"timeWindow,omitempty"`
}

func buildResultSetResolverRequest(resolved *resolvedTurnContext) resultSetResolverRequest {
	return buildResultSetResolverRequestFromTurn(turnFromResolved(resolved))
}

func buildResultSetResolverRequestFromTurn(turn *Turn) resultSetResolverRequest {
	return resultSetResolverRequest{
		Turn:         buildServerTurnRequestFromTurn(turn),
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
	return buildServerExecutionRequestFromTurn(turnFromResolved(resolved), decision)
}

func buildServerExecutionRequestFromTurn(turn *Turn, decision resultSetResolverDecision) serverExecutionRequest {
	if turn == nil {
		return serverExecutionRequest{
			SetKind:               strings.TrimSpace(decision.SetKind),
			Operation:             strings.TrimSpace(decision.Operation),
			SelectionMode:         strings.TrimSpace(decision.SelectionMode),
			SelectionValue:        strings.TrimSpace(decision.SelectionValue),
			CompareSelectionMode:  strings.TrimSpace(decision.CompareSelectionMode),
			CompareSelectionValue: strings.TrimSpace(decision.CompareSelectionValue),
			ItemKey:               strings.TrimSpace(decision.ItemKey),
		}
	}
	request := serverExecutionRequest{
		Domain:                strings.TrimSpace(turn.Normalized.Intent),
		SetKind:               strings.TrimSpace(decision.SetKind),
		Operation:             strings.TrimSpace(decision.Operation),
		SelectionMode:         strings.TrimSpace(decision.SelectionMode),
		SelectionValue:        strings.TrimSpace(decision.SelectionValue),
		CompareSelectionMode:  strings.TrimSpace(decision.CompareSelectionMode),
		CompareSelectionValue: strings.TrimSpace(decision.CompareSelectionValue),
		ItemKey:               strings.TrimSpace(decision.ItemKey),
		TargetName:            strings.TrimSpace(turn.Normalized.TargetName),
		ArtistName:            strings.TrimSpace(turn.Normalized.ArtistName),
		TrackTitle:            strings.TrimSpace(turn.Normalized.TrackTitle),
		PromptHint:            strings.TrimSpace(turn.Normalized.PromptHint),
		TimeWindow:            strings.TrimSpace(turn.Normalized.TimeWindow),
	}
	if request.Domain == "" {
		request.Domain = "other"
	}
	if request.SetKind == "" {
		request.SetKind = strings.TrimSpace(turn.Reference.ResolvedSet)
	}
	if request.Operation == "" {
		request.Operation = strings.TrimSpace(turn.Normalized.ResultAction)
	}
	if request.SelectionMode == "" {
		request.SelectionMode = strings.TrimSpace(turn.Normalized.SelectionMode)
	}
	if request.SelectionValue == "" {
		request.SelectionValue = strings.TrimSpace(turn.Normalized.SelectionValue)
	}
	if request.CompareSelectionMode == "" {
		request.CompareSelectionMode = strings.TrimSpace(turn.Normalized.CompareSelectionMode)
	}
	if request.CompareSelectionValue == "" {
		request.CompareSelectionValue = strings.TrimSpace(turn.Normalized.CompareSelectionValue)
	}
	if request.ItemKey == "" {
		request.ItemKey = strings.TrimSpace(turn.Reference.ResolvedItemKey)
	}
	return request
}

func executionRequestFromTurn(turn *Turn) serverExecutionRequest {
	if turn == nil {
		return serverExecutionRequest{}
	}
	return serverExecutionRequest{
		Domain:                strings.TrimSpace(turn.Execution.Domain),
		SetKind:               strings.TrimSpace(turn.Execution.SetKind),
		Operation:             strings.TrimSpace(turn.Execution.Operation),
		SelectionMode:         strings.TrimSpace(turn.Execution.SelectionMode),
		SelectionValue:        strings.TrimSpace(turn.Execution.SelectionValue),
		CompareSelectionMode:  strings.TrimSpace(turn.Execution.CompareSelectionMode),
		CompareSelectionValue: strings.TrimSpace(turn.Execution.CompareSelectionValue),
		ItemKey:               strings.TrimSpace(turn.Execution.ItemKey),
		TargetName:            strings.TrimSpace(turn.Execution.TargetName),
		ArtistName:            strings.TrimSpace(turn.Execution.ArtistName),
		TrackTitle:            strings.TrimSpace(turn.Execution.TrackTitle),
		PromptHint:            strings.TrimSpace(turn.Execution.PromptHint),
		TimeWindow:            strings.TrimSpace(turn.Execution.TimeWindow),
	}
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
