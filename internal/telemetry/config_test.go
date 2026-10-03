package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

// Config round-trips through the filesystem.
func TestConfigSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, Config{Enabled: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if got, ok := LoadConfig(path); !ok || !got.Enabled {
		t.Errorf("LoadConfig = %+v, %v; want enabled, true", got, ok)
	}
	if err := SaveConfig(path, Config{Enabled: false}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if got, ok := LoadConfig(path); !ok || got.Enabled {
		t.Errorf("LoadConfig = %+v, %v; want disabled, true", got, ok)
	}
}

// A missing config reads as not-ok without error, so an absent file never
// blocks a command and the caller can fall back to the default.
func TestLoadConfigMissing(t *testing.T) {
	if _, ok := LoadConfig(filepath.Join(t.TempDir(), "nope.json")); ok {
		t.Error("missing config should report ok=false")
	}
}

// A corrupt config reports ok=false so callers fall back to the default.
func TestLoadConfigCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, ok := LoadConfig(path); ok {
		t.Error("corrupt config should report ok=false")
	}
}

// The shipped default is enabled.
func TestDefaultConfigEnabled(t *testing.T) {
	if !DefaultConfig().Enabled {
		t.Error("telemetry should be enabled by default")
	}
}

// The config lives beside the database, so --telemetry-db moves both together.
func TestConfigPath(t *testing.T) {
	got := ConfigPath("/tmp/x/telemetry.db")
	if got != "/tmp/x/config.json" {
		t.Errorf("ConfigPath = %q, want /tmp/x/config.json", got)
	}
}

// SaveConfig creates missing parent directories and leaves no temp file behind.
func TestSaveConfigCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	path := filepath.Join(dir, "config.json")
	if err := SaveConfig(path, Config{Enabled: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should be renamed away")
	}
}
