package auth

import (
	"context"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type fixedRNG struct {
	f float64
	n int
}

func (r fixedRNG) Float64() float64 { return r.f }
func (r fixedRNG) IntN(_ int) int   { return r.n }

func TestSuccessRateSelectorPick_TieBreaksRoundRobin(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	selector := NewSuccessRateSelector(1800, 0)
	selector.now = func() time.Time { return now }
	selector.explore = 0

	auths := []*Auth{{ID: "b"}, {ID: "a"}, {ID: "c"}}
	want := []string{"a", "b", "c", "a", "b"}
	for i, id := range want {
		got, err := selector.Pick(context.Background(), "codex", "m", cliproxyexecutor.Options{}, auths)
		if err != nil {
			t.Fatalf("Pick() #%d error = %v", i, err)
		}
		if got == nil {
			t.Fatalf("Pick() #%d auth = nil", i)
		}
		if got.ID != id {
			t.Fatalf("Pick() #%d auth.ID = %q, want %q", i, got.ID, id)
		}
	}
}

func TestSuccessRateSelectorPick_PrefersHigherSuccessRate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	selector := NewSuccessRateSelector(1800, 0)
	selector.now = func() time.Time { return now }
	selector.explore = 0

	for i := 0; i < 5; i++ {
		selector.ObserveResult(Result{AuthID: "a", Provider: "codex", Model: "m", Success: true}, now)
		selector.ObserveResult(Result{AuthID: "b", Provider: "codex", Model: "m", Success: false}, now)
	}

	auths := []*Auth{{ID: "a"}, {ID: "b"}}
	got, err := selector.Pick(context.Background(), "codex", "m", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if got == nil {
		t.Fatalf("Pick() auth = nil")
	}
	if got.ID != "a" {
		t.Fatalf("Pick() auth.ID = %q, want %q", got.ID, "a")
	}
}

func TestSuccessRateSelectorPick_DecaysOldPerformance(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(10 * 30 * time.Minute) // 10 half-lives when half-life=30m

	selector := NewSuccessRateSelector(1800, 0)
	selector.now = func() time.Time { return t1 }
	selector.explore = 0

	selector.ObserveResult(Result{AuthID: "a", Provider: "codex", Model: "m", Success: true}, t0)
	selector.ObserveResult(Result{AuthID: "b", Provider: "codex", Model: "m", Success: true}, t1)

	auths := []*Auth{{ID: "a"}, {ID: "b"}}
	got, err := selector.Pick(context.Background(), "codex", "m", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if got == nil {
		t.Fatalf("Pick() auth = nil")
	}
	if got.ID != "b" {
		t.Fatalf("Pick() auth.ID = %q, want %q", got.ID, "b")
	}
}

func TestSuccessRateSelectorPick_Explores(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	selector := NewSuccessRateSelector(1800, 1)
	selector.now = func() time.Time { return now }
	selector.rng = fixedRNG{f: 0, n: 1} // always explore and choose index 1

	for i := 0; i < 5; i++ {
		selector.ObserveResult(Result{AuthID: "a", Provider: "codex", Model: "m", Success: true}, now)
		selector.ObserveResult(Result{AuthID: "b", Provider: "codex", Model: "m", Success: false}, now)
	}

	auths := []*Auth{{ID: "a"}, {ID: "b"}}
	got, err := selector.Pick(context.Background(), "codex", "m", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if got == nil {
		t.Fatalf("Pick() auth = nil")
	}
	if got.ID != "b" {
		t.Fatalf("Pick() auth.ID = %q, want %q", got.ID, "b")
	}
}
