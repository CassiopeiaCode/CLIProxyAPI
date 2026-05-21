package auth

import (
	"context"
	"math/rand/v2"
	"strings"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

// GoalFirstSelector keeps a session/model-scoped primary target for normal turns
// and reserves other credentials for compaction turns when possible.
type GoalFirstSelector struct {
	cache *SessionCache
}

// GoalFirstSelectorConfig configures session binding retention.
type GoalFirstSelectorConfig struct {
	TTL time.Duration
}

// NewGoalFirstSelector creates a goalfirst selector with default retention.
func NewGoalFirstSelector() *GoalFirstSelector {
	return NewGoalFirstSelectorWithConfig(GoalFirstSelectorConfig{TTL: time.Hour})
}

// NewGoalFirstSelectorWithConfig creates a goalfirst selector with custom retention.
func NewGoalFirstSelectorWithConfig(cfg GoalFirstSelectorConfig) *GoalFirstSelector {
	if cfg.TTL <= 0 {
		cfg.TTL = time.Hour
	}
	return &GoalFirstSelector{
		cache: NewSessionCache(cfg.TTL),
	}
}

// Pick selects a sticky primary auth for normal requests and prefers non-primary
// auths for compaction requests.
func (s *GoalFirstSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	available, err := getAvailableAuths(auths, provider, model, time.Now())
	if err != nil {
		return nil, err
	}

	sessionID := goalFirstSessionID(opts)
	cacheKey := goalFirstBindingKey(sessionID, model)
	if goalFirstIsCompactionRequest(opts) {
		return s.pickCompaction(ctx, provider, cacheKey, available), nil
	}
	return s.pickPrimary(ctx, provider, cacheKey, available), nil
}

func (s *GoalFirstSelector) pickPrimary(ctx context.Context, provider, cacheKey string, available []*Auth) *Auth {
	candidates := goalFirstPrimaryCandidates(ctx, provider, available)
	if len(candidates) == 0 {
		candidates = available
	}
	if cacheKey != "" && s.cache != nil {
		if cachedAuthID, ok := s.cache.GetAndRefresh(cacheKey); ok {
			if cached := goalFirstFindAuthByID(candidates, cachedAuthID); cached != nil {
				return cached
			}
		}
	}
	selected := candidates[goalFirstRandomIndex(len(candidates))]
	if cacheKey != "" && s.cache != nil && selected != nil {
		s.cache.Set(cacheKey, selected.ID)
	}
	return selected
}

func (s *GoalFirstSelector) pickCompaction(ctx context.Context, provider, cacheKey string, available []*Auth) *Auth {
	primaryAuthID := ""
	if cacheKey != "" && s.cache != nil {
		if cachedAuthID, ok := s.cache.GetAndRefresh(cacheKey); ok {
			primaryAuthID = strings.TrimSpace(cachedAuthID)
		}
	}

	candidates := goalFirstSecondaryCandidates(ctx, provider, available, primaryAuthID)
	if len(candidates) == 0 {
		candidates = available
	}
	return candidates[goalFirstRandomIndex(len(candidates))]
}

// Stop releases selector resources.
func (s *GoalFirstSelector) Stop() {
	if s != nil && s.cache != nil {
		s.cache.Stop()
	}
}

func goalFirstPrimaryCandidates(ctx context.Context, provider string, available []*Auth) []*Auth {
	if len(available) == 0 {
		return nil
	}
	codexOnly := make([]*Auth, 0, len(available))
	for _, auth := range available {
		if auth == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
			codexOnly = append(codexOnly, auth)
		}
	}
	if len(codexOnly) > 0 {
		return preferCodexWebsocketAuths(ctx, "codex", codexOnly)
	}
	return preferCodexWebsocketAuths(ctx, provider, available)
}

func goalFirstSecondaryCandidates(ctx context.Context, provider string, available []*Auth, primaryAuthID string) []*Auth {
	if len(available) == 0 {
		return nil
	}
	withoutPrimary := make([]*Auth, 0, len(available))
	for _, auth := range available {
		if auth == nil {
			continue
		}
		if primaryAuthID != "" && auth.ID == primaryAuthID {
			continue
		}
		withoutPrimary = append(withoutPrimary, auth)
	}
	if len(withoutPrimary) == 0 {
		withoutPrimary = available
	}

	nonCodex := make([]*Auth, 0, len(withoutPrimary))
	for _, auth := range withoutPrimary {
		if auth == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(auth.Provider), "codex") {
			nonCodex = append(nonCodex, auth)
		}
	}
	if len(nonCodex) > 0 {
		return nonCodex
	}
	return preferCodexWebsocketAuths(ctx, provider, withoutPrimary)
}

func goalFirstFindAuthByID(auths []*Auth, authID string) *Auth {
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return nil
	}
	for _, auth := range auths {
		if auth != nil && auth.ID == authID {
			return auth
		}
	}
	return nil
}

func goalFirstBindingKey(sessionID, model string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	modelKey := canonicalModelKey(model)
	if modelKey == "" {
		return "goalfirst::" + sessionID
	}
	return "goalfirst::" + sessionID + "::" + modelKey
}

func goalFirstSessionID(opts cliproxyexecutor.Options) string {
	if len(opts.Metadata) != 0 {
		if raw, ok := opts.Metadata[cliproxyexecutor.ExecutionSessionMetadataKey]; ok && raw != nil {
			switch v := raw.(type) {
			case string:
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					return "exec:" + trimmed
				}
			case []byte:
				if trimmed := strings.TrimSpace(string(v)); trimmed != "" {
					return "exec:" + trimmed
				}
			}
		}
	}
	if sessionID := ExtractSessionID(opts.Headers, opts.OriginalRequestBytes(), opts.Metadata); sessionID != "" {
		return sessionID
	}
	body := opts.OriginalRequestBytes()
	if len(body) == 0 {
		return ""
	}
	if convID := strings.TrimSpace(gjson.GetBytes(body, "conversation_id").String()); convID != "" {
		return "conv:" + convID
	}
	return ""
}

func goalFirstIsCompactionRequest(opts cliproxyexecutor.Options) bool {
	if strings.EqualFold(strings.TrimSpace(opts.Alt), "responses/compact") {
		return true
	}
	body := opts.OriginalRequestBytes()
	if len(body) == 0 {
		return false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}
	for _, item := range input.Array() {
		if item.Get("type").String() == "compaction_trigger" {
			return true
		}
	}
	return false
}

func goalFirstRandomIndex(size int) int {
	if size <= 1 {
		return 0
	}
	return rand.IntN(size)
}
