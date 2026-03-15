package auth

import (
	"context"
	"math"
	randv2 "math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

const (
	defaultSuccessRateHalfLifeSeconds = 1800
	defaultSuccessRateExploreRate     = 0.02
)

type successRateState struct {
	success float64
	failure float64
	updated time.Time
}

// ResultObserver can observe execution results and update selector state.
// It is optional; selectors may implement it for feedback-based routing.
type ResultObserver interface {
	ObserveResult(result Result, now time.Time)
}

type successRateRNG interface {
	Float64() float64
	IntN(n int) int
}

type defaultSuccessRateRNG struct{}

func (defaultSuccessRateRNG) Float64() float64 { return randv2.Float64() }
func (defaultSuccessRateRNG) IntN(n int) int   { return randv2.IntN(n) }

// SuccessRateSelector selects credentials by a time-decayed success rate score.
// Score is computed from EMA counts with a neutral Laplace prior:
//
//	score = (success + 1) / (success + failure + 2)
//
// Cooling/disabled/unavailable filtering and priority grouping reuse the existing
// getAvailableAuths helper to avoid "hard blocking" when credentials exist.
type SuccessRateSelector struct {
	mu sync.Mutex

	halfLife   time.Duration
	explore    float64
	maxKeys    int
	now        func() time.Time
	rng        successRateRNG
	state      map[string]successRateState
	rrCursors  map[string]int
	rrFallback *RoundRobinSelector
}

// NewSuccessRateSelector constructs a new selector.
func NewSuccessRateSelector(halfLifeSeconds int, exploreRate float64) *SuccessRateSelector {
	if halfLifeSeconds <= 0 {
		halfLifeSeconds = defaultSuccessRateHalfLifeSeconds
	}
	if exploreRate <= 0 {
		exploreRate = defaultSuccessRateExploreRate
	}
	if exploreRate < 0 {
		exploreRate = 0
	}
	if exploreRate > 1 {
		exploreRate = 1
	}
	return &SuccessRateSelector{
		halfLife:   time.Duration(halfLifeSeconds) * time.Second,
		explore:    exploreRate,
		maxKeys:    4096,
		now:        time.Now,
		rng:        defaultSuccessRateRNG{},
		state:      make(map[string]successRateState),
		rrCursors:  make(map[string]int),
		rrFallback: &RoundRobinSelector{},
	}
}

func (s *SuccessRateSelector) ObserveResult(result Result, now time.Time) {
	if s == nil {
		return
	}
	authID := strings.TrimSpace(result.AuthID)
	model := strings.TrimSpace(result.Model)
	if authID == "" || model == "" {
		return
	}
	provider := strings.ToLower(strings.TrimSpace(result.Provider))
	if provider == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}

	key := s.scoreKey(provider, model, authID)
	s.mu.Lock()
	st := s.state[key]
	st = s.decayLocked(st, now)
	if result.Success {
		st.success += 1
	} else {
		st.failure += 1
	}
	st.updated = now
	s.state[key] = st
	s.mu.Unlock()
}

func (s *SuccessRateSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	_ = opts
	if s == nil {
		return nil, &Error{Code: "auth_not_found", Message: "selector is nil"}
	}

	now := time.Now()
	if s.now != nil {
		now = s.now()
	}

	available, err := getAvailableAuths(auths, provider, model, now)
	if err != nil {
		return nil, err
	}
	available = preferCodexWebsocketAuths(ctx, provider, available)

	// Preserve existing special-case behavior for gemini-cli virtual auth grouping.
	_, parentOrder := groupByVirtualParent(available)
	if len(parentOrder) > 1 {
		return s.rrFallback.Pick(ctx, provider, model, opts, available)
	}

	if len(available) == 1 {
		return available[0], nil
	}

	// Explore with small probability to avoid permanent starvation of new candidates.
	if s.explore > 0 && s.rng != nil && s.rng.Float64() < s.explore {
		idx := 0
		if s.rng != nil {
			idx = s.rng.IntN(len(available))
		}
		return available[idx], nil
	}

	providerKey := strings.ToLower(strings.TrimSpace(provider))
	modelKey := strings.TrimSpace(model)

	type scored struct {
		a     *Auth
		score float64
	}

	scoredList := make([]scored, 0, len(available))
	s.mu.Lock()
	for _, a := range available {
		if a == nil {
			continue
		}
		key := s.scoreKey(providerKey, modelKey, a.ID)
		st := s.state[key]
		score := s.scoreAtLocked(st, now)
		scoredList = append(scoredList, scored{a: a, score: score})
	}
	s.mu.Unlock()

	if len(scoredList) == 0 {
		return nil, &Error{Code: "auth_not_found", Message: "no auth candidates"}
	}

	// Find max score.
	best := scoredList[0].score
	for i := 1; i < len(scoredList); i++ {
		if scoredList[i].score > best {
			best = scoredList[i].score
		}
	}

	// Collect ties (within epsilon) and tie-break via round-robin cursor for stability.
	const eps = 1e-12
	ties := make([]*Auth, 0, len(scoredList))
	for i := range scoredList {
		if math.Abs(scoredList[i].score-best) <= eps {
			ties = append(ties, scoredList[i].a)
		}
	}
	if len(ties) == 1 {
		return ties[0], nil
	}
	sort.Slice(ties, func(i, j int) bool { return ties[i].ID < ties[j].ID })

	rrKey := providerKey + ":" + modelKey
	s.mu.Lock()
	idx := s.ensureRRCursorLocked(rrKey)
	s.rrCursors[rrKey] = idx + 1
	s.mu.Unlock()
	return ties[idx%len(ties)], nil
}

func (s *SuccessRateSelector) ensureRRCursorLocked(key string) int {
	if s.rrCursors == nil {
		s.rrCursors = make(map[string]int)
	}
	limit := s.maxKeys
	if limit <= 0 {
		limit = 4096
	}
	if _, ok := s.rrCursors[key]; !ok && len(s.rrCursors) >= limit {
		s.rrCursors = make(map[string]int)
	}
	idx := s.rrCursors[key]
	if idx >= 2_147_483_640 {
		idx = 0
	}
	return idx
}

func (s *SuccessRateSelector) scoreKey(provider, model, authID string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + ":" + strings.TrimSpace(model) + ":" + strings.TrimSpace(authID)
}

func (s *SuccessRateSelector) decayLocked(st successRateState, now time.Time) successRateState {
	if s.halfLife <= 0 {
		return st
	}
	if st.updated.IsZero() || now.IsZero() || !now.After(st.updated) {
		return st
	}
	dt := now.Sub(st.updated)
	if dt <= 0 {
		return st
	}
	decay := math.Pow(0.5, float64(dt)/float64(s.halfLife))
	if decay < 0 {
		decay = 0
	}
	if decay > 1 {
		decay = 1
	}
	st.success *= decay
	st.failure *= decay
	st.updated = now
	return st
}

func (s *SuccessRateSelector) scoreAtLocked(st successRateState, now time.Time) float64 {
	// Apply decay lazily for scoring without mutating state.
	hl := s.halfLife
	success := st.success
	failure := st.failure
	if hl > 0 && !st.updated.IsZero() && !now.IsZero() && now.After(st.updated) {
		dt := now.Sub(st.updated)
		if dt > 0 {
			decay := math.Pow(0.5, float64(dt)/float64(hl))
			if decay < 0 {
				decay = 0
			}
			if decay > 1 {
				decay = 1
			}
			success *= decay
			failure *= decay
		}
	}
	// Neutral Laplace prior: (s+1)/(s+f+2)
	return (success + 1) / (success + failure + 2)
}
