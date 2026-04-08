package config

import "testing"

func TestAuthRefreshEnabled_DefaultsToTrue(t *testing.T) {
	var cfg Config
	if !cfg.AuthRefreshEnabled() {
		t.Fatalf("AuthRefreshEnabled() = false, want true")
	}
}

func TestAuthRefreshEnabled_DisableAuthRefreshTurnsItOff(t *testing.T) {
	cfg := Config{DisableAuthRefresh: true}
	if cfg.AuthRefreshEnabled() {
		t.Fatalf("AuthRefreshEnabled() = true, want false")
	}
}
