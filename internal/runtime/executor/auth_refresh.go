package executor

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

func authRefreshEnabled(cfg *config.Config) bool {
	return cfg == nil || cfg.AuthRefreshEnabled()
}

func skipRefreshWhenDisabled(cfg *config.Config, provider string, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, bool) {
	if authRefreshEnabled(cfg) {
		return auth, false
	}
	log.Debugf("%s executor: auth refresh disabled by config", provider)
	return auth, true
}
