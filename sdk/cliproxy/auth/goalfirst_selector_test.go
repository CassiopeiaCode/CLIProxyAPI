package auth

import (
	"context"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestGoalFirstSelectorPinsNormalTurnsToCodexAuth(t *testing.T) {
	t.Parallel()

	selector := NewGoalFirstSelectorWithConfig(GoalFirstSelectorConfig{TTL: time.Minute})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "gemini-a", Provider: "gemini"},
		{ID: "codex-a", Provider: "codex"},
		{ID: "codex-b", Provider: "codex"},
	}
	opts := cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.ExecutionSessionMetadataKey: "session-1",
		},
	}

	first, err := selector.Pick(context.Background(), "mixed", "gpt-5", opts, auths)
	if err != nil {
		t.Fatalf("first Pick() error = %v", err)
	}
	if first == nil {
		t.Fatal("first Pick() auth = nil")
	}
	if first.Provider != "codex" {
		t.Fatalf("first Pick() provider = %q, want codex", first.Provider)
	}

	for i := 0; i < 10; i++ {
		got, err := selector.Pick(context.Background(), "mixed", "gpt-5", opts, auths)
		if err != nil {
			t.Fatalf("repeat Pick() #%d error = %v", i, err)
		}
		if got == nil {
			t.Fatalf("repeat Pick() #%d auth = nil", i)
		}
		if got.ID != first.ID {
			t.Fatalf("repeat Pick() #%d auth.ID = %q, want sticky %q", i, got.ID, first.ID)
		}
	}
}

func TestGoalFirstSelectorRoutesCompactionToNonPrimaryAuth(t *testing.T) {
	t.Parallel()

	selector := NewGoalFirstSelectorWithConfig(GoalFirstSelectorConfig{TTL: time.Minute})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "codex-a", Provider: "codex"},
		{ID: "gemini-a", Provider: "gemini"},
	}
	normalOpts := cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.ExecutionSessionMetadataKey: "session-2",
		},
	}

	primary, err := selector.Pick(context.Background(), "mixed", "gpt-5", normalOpts, auths)
	if err != nil {
		t.Fatalf("normal Pick() error = %v", err)
	}
	if primary == nil {
		t.Fatal("normal Pick() auth = nil")
	}
	if primary.ID != "codex-a" {
		t.Fatalf("normal Pick() auth.ID = %q, want codex-a", primary.ID)
	}

	compactOpts := cliproxyexecutor.Options{
		Alt: "responses/compact",
		Metadata: map[string]any{
			cliproxyexecutor.ExecutionSessionMetadataKey: "session-2",
		},
	}
	compaction, err := selector.Pick(context.Background(), "mixed", "gpt-5", compactOpts, auths)
	if err != nil {
		t.Fatalf("compaction Pick() error = %v", err)
	}
	if compaction == nil {
		t.Fatal("compaction Pick() auth = nil")
	}
	if compaction.ID != "gemini-a" {
		t.Fatalf("compaction Pick() auth.ID = %q, want gemini-a", compaction.ID)
	}
}

func TestGoalFirstSelectorUsesModelScopedBindings(t *testing.T) {
	t.Parallel()

	selector := NewGoalFirstSelectorWithConfig(GoalFirstSelectorConfig{TTL: time.Minute})
	defer selector.Stop()

	opts := cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.ExecutionSessionMetadataKey: "session-3",
		},
	}

	modelAAuths := []*Auth{
		{ID: "codex-a", Provider: "codex"},
	}
	modelBAuths := []*Auth{
		{ID: "codex-b", Provider: "codex"},
	}

	pickedA, err := selector.Pick(context.Background(), "mixed", "model-a", opts, modelAAuths)
	if err != nil {
		t.Fatalf("Pick(model-a) error = %v", err)
	}
	if pickedA == nil || pickedA.ID != "codex-a" {
		t.Fatalf("Pick(model-a) = %#v, want codex-a", pickedA)
	}

	pickedB, err := selector.Pick(context.Background(), "mixed", "model-b", opts, modelBAuths)
	if err != nil {
		t.Fatalf("Pick(model-b) error = %v", err)
	}
	if pickedB == nil || pickedB.ID != "codex-b" {
		t.Fatalf("Pick(model-b) = %#v, want codex-b", pickedB)
	}

	pickedA2, err := selector.Pick(context.Background(), "mixed", "model-a", opts, modelAAuths)
	if err != nil {
		t.Fatalf("Pick(model-a again) error = %v", err)
	}
	if pickedA2 == nil || pickedA2.ID != "codex-a" {
		t.Fatalf("Pick(model-a again) = %#v, want codex-a", pickedA2)
	}
}

func TestGoalFirstSelectorTreatsCompactionTriggerAsCompaction(t *testing.T) {
	t.Parallel()

	selector := NewGoalFirstSelectorWithConfig(GoalFirstSelectorConfig{TTL: time.Minute})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "codex-a", Provider: "codex"},
		{ID: "claude-a", Provider: "claude"},
	}
	sessionMeta := map[string]any{
		cliproxyexecutor.ExecutionSessionMetadataKey: "session-4",
	}

	_, err := selector.Pick(context.Background(), "mixed", "gpt-5", cliproxyexecutor.Options{
		Metadata: sessionMeta,
	}, auths)
	if err != nil {
		t.Fatalf("normal Pick() error = %v", err)
	}

	got, err := selector.Pick(context.Background(), "mixed", "gpt-5", cliproxyexecutor.Options{
		OriginalRequest: []byte(`{"input":[{"type":"compaction_trigger"}]}`),
		Metadata:        sessionMeta,
	}, auths)
	if err != nil {
		t.Fatalf("compaction-trigger Pick() error = %v", err)
	}
	if got == nil || got.ID != "claude-a" {
		t.Fatalf("compaction-trigger Pick() = %#v, want claude-a", got)
	}
}
