package main

import (
	"context"
	"testing"
	"time"
)

func TestConversationalApproveExecutesLatestPendingAction(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	action := srv.registerPendingAction("sess-approve", "test", "Test", "Approve this", nil, func(context.Context) (string, error) {
		return "approved ok", nil
	})
	if action == nil {
		t.Fatal("expected pending action")
	}

	ctx := context.WithValue(context.Background(), chatSessionKey, "sess-approve")
	resp, ok := srv.tryConversationalPendingAction(ctx, "yes")
	if !ok {
		t.Fatal("expected conversational approval to be handled")
	}
	if resp.Response != "approved ok" {
		t.Fatalf("response = %q, want %q", resp.Response, "approved ok")
	}
	if got := srv.latestPendingAction("sess-approve"); got != nil {
		t.Fatalf("expected pending action to be cleared, got %#v", got)
	}
}

func TestConversationalDiscardClearsLatestPendingAction(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	srv.registerPendingAction("sess-discard", "test", "Test", "Discard this", nil, func(context.Context) (string, error) {
		return "should not run", nil
	})

	ctx := context.WithValue(context.Background(), chatSessionKey, "sess-discard")
	resp, ok := srv.tryConversationalPendingAction(ctx, "no")
	if !ok {
		t.Fatal("expected conversational discard to be handled")
	}
	if resp.Response != "Request discarded." {
		t.Fatalf("response = %q, want %q", resp.Response, "Request discarded.")
	}
	if got := srv.latestPendingAction("sess-discard"); got != nil {
		t.Fatalf("expected pending action to be cleared, got %#v", got)
	}
}

func TestConversationalApproveWithoutPendingAction(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	ctx := context.WithValue(context.Background(), chatSessionKey, "sess-empty")
	resp, ok := srv.tryConversationalPendingAction(ctx, "yes")
	if !ok {
		t.Fatal("expected conversational approval to be handled")
	}
	if resp.Response == "" {
		t.Fatal("expected explanatory response when no pending action exists")
	}
}

func TestRegisterPendingActionReplacesLatestPointerWithinSession(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	first := srv.registerPendingAction("sess-replace", "test", "First", "first", nil, func(context.Context) (string, error) {
		return "first", nil
	})
	second := srv.registerPendingAction("sess-replace", "test", "Second", "second", nil, func(context.Context) (string, error) {
		return "second", nil
	})
	if first == nil || second == nil {
		t.Fatal("expected pending actions")
	}
	if got := srv.latestPendingAction("sess-replace"); got == nil || got.ID != second.ID {
		t.Fatalf("latestPendingAction() = %#v, want second action %#v", got, second)
	}
	if len(srv.approvals) != 2 {
		t.Fatalf("approvals size = %d, want 2 retained actions", len(srv.approvals))
	}
}

func TestConversationalApproveIsSessionScoped(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	action := srv.registerPendingAction("sess-a", "test", "Scoped", "session a only", nil, func(context.Context) (string, error) {
		return "approved a", nil
	})
	if action == nil {
		t.Fatal("expected pending action")
	}
	ctx := context.WithValue(context.Background(), chatSessionKey, "sess-b")
	resp, ok := srv.tryConversationalPendingAction(ctx, "yes")
	if !ok {
		t.Fatal("expected conversational approval to be handled")
	}
	if resp.Response != "There isn't a pending action to approve right now." {
		t.Fatalf("response = %q", resp.Response)
	}
	if got := srv.latestPendingAction("sess-a"); got == nil || got.ID != action.ID {
		t.Fatalf("session-a pending action was unexpectedly changed: %#v", got)
	}
}

func TestConversationalApproveExpiredActionClearsState(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	expired := PendingAction{
		ID:        "act_expired",
		Kind:      "test",
		Title:     "Expired",
		Summary:   "expired",
		ExpiresAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	}
	srv.approvals[expired.ID] = &pendingActionState{
		payload:   expired,
		execute:   func(context.Context) (string, error) { return "should not run", nil },
		sessionID: "sess-expired",
		createdAt: time.Now().UTC().Add(-2 * time.Minute),
	}
	srv.latestPending["sess-expired"] = expired.ID

	ctx := context.WithValue(context.Background(), chatSessionKey, "sess-expired")
	resp, ok := srv.tryConversationalPendingAction(ctx, "yes")
	if !ok {
		t.Fatal("expected conversational approval to be handled")
	}
	if resp.Response != "There isn't a pending action to approve right now." {
		t.Fatalf("response = %q", resp.Response)
	}
	if got := srv.latestPendingAction("sess-expired"); got != nil {
		t.Fatalf("expected expired pending action to be cleared, got %#v", got)
	}
}

func TestConversationalDiscardWithoutPendingAction(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	ctx := context.WithValue(context.Background(), chatSessionKey, "sess-empty-discard")
	resp, ok := srv.tryConversationalPendingAction(ctx, "no")
	if !ok {
		t.Fatal("expected conversational discard to be handled")
	}
	if resp.Response != "There isn't a pending action to discard right now." {
		t.Fatalf("response = %q", resp.Response)
	}
}

func TestRegisterPendingActionSetsExpectedExpiryWindow(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	before := time.Now().UTC()
	action := srv.registerPendingAction("sess-expiry", "test", "Expiry", "check expiry", nil, func(context.Context) (string, error) {
		return "ok", nil
	})
	if action == nil {
		t.Fatal("expected pending action")
	}
	expiresAt, err := time.Parse(time.RFC3339, action.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expiresAt: %v", err)
	}
	min := before.Add(pendingActionTTL() - 5*time.Second)
	max := before.Add(pendingActionTTL() + 5*time.Second)
	if expiresAt.Before(min) || expiresAt.After(max) {
		t.Fatalf("expiresAt = %v, want between %v and %v", expiresAt, min, max)
	}
}

func TestLatestPendingActionSinceHonorsCreationTime(t *testing.T) {
	srv := &Server{
		approvals:     make(map[string]*pendingActionState),
		latestPending: make(map[string]string),
	}
	action := srv.registerPendingAction("sess-since", "test", "Since", "created recently", nil, func(context.Context) (string, error) {
		return "ok", nil
	})
	if action == nil {
		t.Fatal("expected pending action")
	}
	state := srv.approvals[action.ID]
	if state == nil {
		t.Fatal("expected stored state")
	}
	if got := srv.latestPendingActionSince("sess-since", state.createdAt.Add(-time.Second)); got == nil || got.ID != action.ID {
		t.Fatalf("latestPendingActionSince(before createdAt) = %#v, want %#v", got, action)
	}
	if got := srv.latestPendingActionSince("sess-since", state.createdAt.Add(time.Second)); got != nil {
		t.Fatalf("latestPendingActionSince(after createdAt) = %#v, want nil", got)
	}
}
