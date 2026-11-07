package gh

import (
	"reflect"
	"testing"
)

func withTempStash(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	old := stashBaseDir
	stashBaseDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { stashBaseDir = old })
}

func TestStashRoundTrip(t *testing.T) {
	withTempStash(t)

	line := 7
	first := []SavedThread{{
		Path:   "a.go",
		Line:   &line,
		Side:   "RIGHT",
		Bodies: []string{"first comment"},
	}}
	second := []SavedThread{{
		Path:   "b.go",
		Side:   "LEFT",
		Bodies: []string{"second comment", "another reply"},
	}}

	n, err := PushStash("acme", "demo", 1, first, "first stash")
	if err != nil {
		t.Fatalf("PushStash first: %v", err)
	}
	if n != 1 {
		t.Errorf("stash count after first push = %d, want 1", n)
	}

	n, err = PushStash("acme", "demo", 1, second, "second stash")
	if err != nil {
		t.Fatalf("PushStash second: %v", err)
	}
	if n != 2 {
		t.Errorf("stash count after second push = %d, want 2", n)
	}

	entries, err := ListStashEntries("acme", "demo", 1)
	if err != nil {
		t.Fatalf("ListStashEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("stash entries = %d, want 2", len(entries))
	}
	if entries[0].Message != "second stash" || entries[1].Message != "first stash" {
		t.Errorf("stash order = [%q, %q], want newest first", entries[0].Message, entries[1].Message)
	}

	got, err := GetStashEntry("acme", "demo", 1, 0)
	if err != nil {
		t.Fatalf("GetStashEntry: %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Errorf("stash@0 = %+v, want %+v", got, second)
	}

	popped, err := PopStash("acme", "demo", 1, 0)
	if err != nil {
		t.Fatalf("PopStash: %v", err)
	}
	if !reflect.DeepEqual(popped, second) {
		t.Errorf("popped = %+v, want %+v", popped, second)
	}

	entries, err = ListStashEntries("acme", "demo", 1)
	if err != nil {
		t.Fatalf("ListStashEntries after pop: %v", err)
	}
	if len(entries) != 1 || entries[0].Message != "first stash" {
		t.Fatalf("stash after pop = %+v, want only the first entry", entries)
	}

	if err := RemoveStashEntry("acme", "demo", 1, 0); err != nil {
		t.Fatalf("RemoveStashEntry: %v", err)
	}
	if _, err := ListStashEntries("acme", "demo", 1); err == nil {
		t.Fatal("expected an error once the last stash entry is removed")
	}
}

func TestStashIndexOutOfRange(t *testing.T) {
	withTempStash(t)

	if _, err := PushStash("acme", "demo", 1, []SavedThread{{Path: "a.go"}}, ""); err != nil {
		t.Fatalf("PushStash: %v", err)
	}

	if _, err := GetStashEntry("acme", "demo", 1, 3); err == nil {
		t.Fatal("expected an error for an index past the last stash entry")
	}
	if err := RemoveStashEntry("acme", "demo", 1, 3); err == nil {
		t.Fatal("expected an error removing an index past the last stash entry")
	}
}

func TestStashMissing(t *testing.T) {
	withTempStash(t)

	if _, err := ListStashEntries("acme", "demo", 1); err == nil {
		t.Fatal("expected an error when no stash exists")
	}
}
