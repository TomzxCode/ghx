package gh

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

type StashEntry struct {
	Threads []SavedThread `yaml:"threads"`
	Message string        `yaml:"message,omitempty"`
}

func StashDir(owner, name string, number int) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve cache dir: %w", err)
	}

	return filepath.Join(cacheDir, "ghx", "stash", owner, name, fmt.Sprintf("%d", number)), nil
}

func listStashFiles(dir string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var indices []int
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		base := e.Name()[:len(e.Name())-len(ext)]
		idx, err := strconv.Atoi(base)
		if err != nil {
			continue
		}
		indices = append(indices, idx)
	}

	sort.Ints(indices)
	return indices, nil
}

func stashEntryPath(dir string, index int) string {
	return filepath.Join(dir, fmt.Sprintf("%d.yaml", index))
}

func loadStashEntry(path string) (StashEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return StashEntry{}, err
	}

	var entry StashEntry
	if err := yaml.Unmarshal(data, &entry); err != nil {
		return StashEntry{}, fmt.Errorf("parse stash: %w", err)
	}

	return entry, nil
}

func saveStashEntry(path string, entry StashEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create stash dir: %w", err)
	}

	data, err := yaml.Marshal(&entry)
	if err != nil {
		return fmt.Errorf("marshal stash: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write stash: %w", err)
	}

	return nil
}

func ListStashEntries(owner, name string, number int) ([]StashEntry, error) {
	dir, err := StashDir(owner, name, number)
	if err != nil {
		return nil, err
	}

	indices, err := listStashFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no stash found for %s/%s#%d", owner, name, number)
		}
		return nil, fmt.Errorf("read stash dir: %w", err)
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("no stash found for %s/%s#%d", owner, name, number)
	}

	entries := make([]StashEntry, 0, len(indices))
	for _, idx := range indices {
		entry, err := loadStashEntry(stashEntryPath(dir, idx))
		if err != nil {
			return nil, fmt.Errorf("read stash@{%d}: %w", idx, err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func PushStash(owner, name string, number int, threads []SavedThread, message string) (int, error) {
	dir, err := StashDir(owner, name, number)
	if err != nil {
		return 0, err
	}

	indices, _ := listStashFiles(dir)

	for i := len(indices) - 1; i >= 0; i-- {
		oldPath := stashEntryPath(dir, indices[i])
		newPath := stashEntryPath(dir, indices[i]+1)
		if err := os.Rename(oldPath, newPath); err != nil {
			return 0, fmt.Errorf("shift stash@{%d}: %w", indices[i], err)
		}
	}

	entry := StashEntry{Threads: threads, Message: message}
	if err := saveStashEntry(stashEntryPath(dir, 0), entry); err != nil {
		return 0, err
	}

	return len(indices) + 1, nil
}

func GetStashEntry(owner, name string, number int, index int) ([]SavedThread, error) {
	dir, err := StashDir(owner, name, number)
	if err != nil {
		return nil, err
	}

	indices, err := listStashFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no stash found for %s/%s#%d", owner, name, number)
		}
		return nil, fmt.Errorf("read stash dir: %w", err)
	}

	if index < 0 || index >= len(indices) {
		return nil, fmt.Errorf("stash@{%d} does not exist (have %d stash entries)", index, len(indices))
	}

	entry, err := loadStashEntry(stashEntryPath(dir, indices[index]))
	if err != nil {
		return nil, fmt.Errorf("read stash@{%d}: %w", index, err)
	}

	return entry.Threads, nil
}

func RemoveStashEntry(owner, name string, number int, index int) error {
	dir, err := StashDir(owner, name, number)
	if err != nil {
		return err
	}

	indices, err := listStashFiles(dir)
	if err != nil {
		return err
	}

	if index < 0 || index >= len(indices) {
		return fmt.Errorf("stash@{%d} does not exist (have %d stash entries)", index, len(indices))
	}

	if err := os.Remove(stashEntryPath(dir, indices[index])); err != nil {
		return fmt.Errorf("remove stash@{%d}: %w", index, err)
	}

	for i := index + 1; i < len(indices); i++ {
		oldPath := stashEntryPath(dir, indices[i])
		newPath := stashEntryPath(dir, indices[i]-1)
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("shift stash@{%d}: %w", indices[i], err)
		}
	}

	remaining, _ := listStashFiles(dir)
	if len(remaining) == 0 {
		os.Remove(dir)
	}

	return nil
}

func PopStash(owner, name string, number int, index int) ([]SavedThread, error) {
	threads, err := GetStashEntry(owner, name, number, index)
	if err != nil {
		return nil, err
	}

	if err := RemoveStashEntry(owner, name, number, index); err != nil {
		return nil, err
	}

	return threads, nil
}

func DropStash(owner, name string, number int, index int) error {
	_, err := PopStash(owner, name, number, index)
	return err
}

func AppendToStash(owner, name string, number int, stashIndex int, thread SavedThread) (int, error) {
	dir, err := StashDir(owner, name, number)
	if err != nil {
		return 0, err
	}

	indices, _ := listStashFiles(dir)

	if len(indices) == 0 {
		if stashIndex != 0 {
			return 0, fmt.Errorf("stash@{%d} does not exist (no stash entries)", stashIndex)
		}
		entry := StashEntry{Threads: []SavedThread{thread}}
		if err := saveStashEntry(stashEntryPath(dir, 0), entry); err != nil {
			return 0, err
		}
		return 1, nil
	}

	if stashIndex < 0 || stashIndex >= len(indices) {
		return 0, fmt.Errorf("stash@{%d} does not exist (have %d stash entries)", stashIndex, len(indices))
	}

	entry, err := loadStashEntry(stashEntryPath(dir, indices[stashIndex]))
	if err != nil {
		return 0, fmt.Errorf("read stash@{%d}: %w", stashIndex, err)
	}

	entry.Threads = append(entry.Threads, thread)

	if err := saveStashEntry(stashEntryPath(dir, indices[stashIndex]), entry); err != nil {
		return 0, err
	}

	return len(entry.Threads), nil
}

func ClearStash(owner, name string, number int) error {
	dir, err := StashDir(owner, name, number)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stash dir: %w", err)
	}

	return nil
}
