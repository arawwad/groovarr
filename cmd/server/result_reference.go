package main

import "strings"

type resultSelection struct {
	Mode  string
	Value string
}

type resultReference struct {
	SetKind            string
	Action             string
	Selection          resultSelection
	Target             string
	Qualifier          string
	NeedsClarification bool
}

type resolvedResultReference struct {
	resultReference
	ResolvedSetKind string
	ResolvedSource  string
	ResolvedItemKey string
	ResolvedItemRef string
	Ambiguous       bool
}

func (t normalizedTurn) resultReference() resultReference {
	return resultReference{
		SetKind:   strings.TrimSpace(t.ResultSetKind),
		Action:    strings.TrimSpace(t.ResultAction),
		Selection: resultSelection{Mode: strings.TrimSpace(t.SelectionMode), Value: strings.TrimSpace(t.SelectionValue)},
		Target:    strings.TrimSpace(t.ReferenceTarget),
		Qualifier: strings.TrimSpace(t.ReferenceQualifier),
	}
}

func (r *resolvedTurnContext) resultReference() resolvedResultReference {
	if r == nil {
		return resolvedResultReference{}
	}
	base := r.Turn.resultReference()
	base.NeedsClarification = r.Turn.NeedsClarification
	return resolvedResultReference{
		resultReference: base,
		ResolvedSetKind: strings.TrimSpace(r.ResolvedReferenceKind),
		ResolvedSource:  strings.TrimSpace(r.ResolvedReferenceSource),
		ResolvedItemKey: strings.TrimSpace(r.ResolvedItemKey),
		ResolvedItemRef: strings.TrimSpace(r.ResolvedItemSource),
		Ambiguous:       r.AmbiguousReference,
	}
}

func (r resolvedResultReference) effectiveSetKind() string {
	if r.ResolvedSetKind != "" {
		return r.ResolvedSetKind
	}
	return r.SetKind
}
