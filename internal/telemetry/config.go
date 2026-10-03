package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ConfigFileName is the persisted global telemetry setting, stored next to the
// telemetry database.
const ConfigFileName = "config.json"

// Config is the persisted global telemetry preference, set by
// `ghx telemetry enable|disable`.
type Config struct {
	// Enabled controls whether recording happens by default. A pointer is not
	// used so a missing file reads as disabled; the zero value is the shipped
	// default.
	Enabled bool `json:"enabled"`
}

// ConfigPath resolves where the config is stored, honoring the same directory
// precedence as the database: an explicit db path places the config beside it.
func ConfigPath(dbPath string) string {
	if dbPath == "" {
		dbPath = DefaultPath()
	}
	return filepath.Join(filepath.Dir(dbPath), ConfigFileName)
}

// LoadConfig reads the persisted preference. A missing or unreadable file
// yields the zero value and ok=false, so callers can fall back to the default
// instead of treating corruption as an explicit "disabled".
func LoadConfig(path string) (cfg Config, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, false
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false
	}
	return cfg, true
}

// SaveConfig writes the preference atomically, creating the directory if
// needed.
func SaveConfig(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DefaultConfig returns the shipped default. Telemetry is enabled by default so
// performance data accumulates without anyone opting in; it stays local-only and
// `ghx telemetry disable` turns it off.
func DefaultConfig() Config {
	return Config{Enabled: true}
}
