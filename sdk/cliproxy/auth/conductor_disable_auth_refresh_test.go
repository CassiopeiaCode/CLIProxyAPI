package auth

import (
	"context"
	"net/http"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type refreshProbeExecutor struct {
	calls int
}

func (e *refreshProbeExecutor) Identifier() string {
	return "codex"
}

func (e *refreshProbeExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e *refreshProbeExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}

func (e *refreshProbeExecutor) Refresh(_ context.Context, auth *Auth) (*Auth, error) {
	e.calls++
	return auth, nil
}

func (e *refreshProbeExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}

func (e *refreshProbeExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestRefreshAuth_SkipsWhenAuthRefreshDisabled(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	mgr.runtimeConfig.Store(&internalconfig.Config{DisableAuthRefresh: true})

	auth := &Auth{ID: "auth-1", Provider: "codex"}
	mgr.auths[auth.ID] = auth

	exec := &refreshProbeExecutor{}
	mgr.executors[auth.Provider] = exec

	mgr.refreshAuth(context.Background(), auth.ID)

	if exec.calls != 0 {
		t.Fatalf("refresh calls = %d, want 0", exec.calls)
	}
}

func TestRefreshAuth_CallsExecutorWhenAuthRefreshEnabled(t *testing.T) {
	mgr := NewManager(nil, nil, nil)
	mgr.runtimeConfig.Store(&internalconfig.Config{})

	auth := &Auth{ID: "auth-1", Provider: "codex"}
	mgr.auths[auth.ID] = auth

	exec := &refreshProbeExecutor{}
	mgr.executors[auth.Provider] = exec

	mgr.refreshAuth(context.Background(), auth.ID)

	if exec.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", exec.calls)
	}
}
