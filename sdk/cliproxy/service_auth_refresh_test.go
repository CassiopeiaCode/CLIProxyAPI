package cliproxy

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestStartCoreAuthAutoRefresh_EnabledStartsLoop(t *testing.T) {
	svc := &Service{
		cfg:         &config.Config{},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}

	if started := svc.startCoreAuthAutoRefresh(); !started {
		t.Fatalf("startCoreAuthAutoRefresh() = false, want true")
	}

	svc.coreManager.StopAutoRefresh()
}

func TestStartCoreAuthAutoRefresh_DisabledSkipsLoop(t *testing.T) {
	svc := &Service{
		cfg:         &config.Config{DisableAuthRefresh: true},
		coreManager: coreauth.NewManager(nil, nil, nil),
	}

	if started := svc.startCoreAuthAutoRefresh(); started {
		t.Fatalf("startCoreAuthAutoRefresh() = true, want false")
	}
}
