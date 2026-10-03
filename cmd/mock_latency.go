package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tomzxcode/ghx/internal/mockserver"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// loadLatencyModel fits a mock-server latency model from a source path. The path
// may be a telemetry SQLite database (as written by --telemetry) or a JSON file
// previously produced by `ghx telemetry export`, so the export/consume loop
// stays explicit and inspectable.
func loadLatencyModel(path string) (*mockserver.LatencyModel, error) {
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		return loadLatencyModelJSON(path)
	}
	store, err := telemetry.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening telemetry data at %s: %w", path, err)
	}
	defer store.Close()
	events, err := store.Query(telemetry.Filter{})
	if err != nil {
		return nil, fmt.Errorf("reading telemetry data: %w", err)
	}
	return mockserver.NewLatencyModel(events, 42), nil
}

func loadLatencyModelJSON(path string) (*mockserver.LatencyModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var events []telemetry.StoredEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return mockserver.NewLatencyModel(events, 42), nil
}
