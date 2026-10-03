package config

import (
	"strings"
	"testing"
)

func TestParseRuntimeSettingsDefaults(t *testing.T) {
	clearRuntimeSettingsEnv(t)

	settings, err := ParseRuntimeSettings()
	if err != nil {
		t.Fatalf("ParseRuntimeSettings() error = %v", err)
	}

	if settings.RunMigration {
		t.Fatal("RunMigration = true, want false")
	}
	if !settings.HTTPServerEnabled {
		t.Fatal("HTTPServerEnabled = false, want true")
	}
	if !settings.RSSWorkerEnabled {
		t.Fatal("RSSWorkerEnabled = false, want true")
	}
	if settings.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want %q", settings.HTTPAddr, ":8080")
	}
}

func TestParseRuntimeSettingsServerDisabledWorkerEnabled(t *testing.T) {
	clearRuntimeSettingsEnv(t)
	t.Setenv(EnvHTTPServerEnabled, "false")
	t.Setenv(EnvRSSWorkerEnabled, "true")

	settings, err := ParseRuntimeSettings()
	if err != nil {
		t.Fatalf("ParseRuntimeSettings() error = %v", err)
	}

	if settings.HTTPServerEnabled {
		t.Fatal("HTTPServerEnabled = true, want false")
	}
	if !settings.RSSWorkerEnabled {
		t.Fatal("RSSWorkerEnabled = false, want true")
	}
}

func TestParseRuntimeSettingsWorkerDisabledServerEnabled(t *testing.T) {
	clearRuntimeSettingsEnv(t)
	t.Setenv(EnvHTTPServerEnabled, "true")
	t.Setenv(EnvRSSWorkerEnabled, "false")

	settings, err := ParseRuntimeSettings()
	if err != nil {
		t.Fatalf("ParseRuntimeSettings() error = %v", err)
	}

	if !settings.HTTPServerEnabled {
		t.Fatal("HTTPServerEnabled = false, want true")
	}
	if settings.RSSWorkerEnabled {
		t.Fatal("RSSWorkerEnabled = true, want false")
	}
}

func TestParseRuntimeSettingsBothComponentsDisabled(t *testing.T) {
	clearRuntimeSettingsEnv(t)
	t.Setenv(EnvHTTPServerEnabled, "false")
	t.Setenv(EnvRSSWorkerEnabled, "false")

	_, err := ParseRuntimeSettings()
	if err == nil {
		t.Fatal("ParseRuntimeSettings() error = nil, want error")
	}
	if !strings.Contains(err.Error(), EnvHTTPServerEnabled) || !strings.Contains(err.Error(), EnvRSSWorkerEnabled) {
		t.Fatalf("ParseRuntimeSettings() error = %q, want both component env names", err.Error())
	}
}

func TestParseRuntimeSettingsHTTPAddr(t *testing.T) {
	clearRuntimeSettingsEnv(t)
	t.Setenv(EnvHTTPAddr, ":9090")

	settings, err := ParseRuntimeSettings()
	if err != nil {
		t.Fatalf("ParseRuntimeSettings() error = %v", err)
	}

	if settings.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q, want %q", settings.HTTPAddr, ":9090")
	}
}

func clearRuntimeSettingsEnv(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		EnvRunMigration,
		EnvHTTPServerEnabled,
		EnvRSSWorkerEnabled,
		EnvHTTPAddr,
	} {
		t.Setenv(name, "")
	}
}
