package gh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func StashPath(owner, name string, number int) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}

	dir := filepath.Join(cacheDir, "ghx", "stash", owner, name)
	return filepath.Join(dir, fmt.Sprintf("%d.json", number)), nil
}

func SaveStash(owner, name string, number int, threads []SavedThread) error {
	path, err := StashPath(owner, name, number)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create stash dir: %w", err)
	}

	data, err := json.MarshalIndent(threads, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stash: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write stash: %w", err)
	}

	return nil
}

func LoadStash(owner, name string, number int) ([]SavedThread, error) {
	path, err := StashPath(owner, name, number)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no stash found for %s/%s#%d", owner, name, number)
		}
		return nil, fmt.Errorf("read stash: %w", err)
	}

	var threads []SavedThread
	if err := json.Unmarshal(data, &threads); err != nil {
		return nil, fmt.Errorf("parse stash: %w", err)
	}

	return threads, nil
}

func ClearStash(owner, name string, number int) error {
	path, err := StashPath(owner, name, number)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stash: %w", err)
	}

	return nil
}
