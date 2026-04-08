package executor

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestCodexExecutorRefresh_SkipsWhenAuthRefreshDisabled(t *testing.T) {
	exec := NewCodexExecutor(&config.Config{DisableAuthRefresh: true})
	auth := &cliproxyauth.Auth{
		Metadata: map[string]any{
			"refresh_token": "rt_test",
		},
	}

	updated, err := exec.Refresh(context.Background(), auth)
	if err != nil {
		t.Fatalf("Refresh() error = %v, want nil", err)
	}
	if updated != auth {
		t.Fatalf("Refresh() returned cloned auth, want original pointer")
	}
	if _, ok := updated.Metadata["last_refresh"]; ok {
		t.Fatalf("last_refresh should not be set when refresh is disabled")
	}
}
