package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestApplyThreadStateContinuesAfterFailure(t *testing.T) {
	var called []string
	var out bytes.Buffer

	failures := map[string]bool{"thread-2": true}
	set := func(id string) error {
		called = append(called, id)
		if failures[id] {
			return errors.New("not found")
		}
		return nil
	}

	err := applyThreadState(&out, []string{"thread-1", "thread-2", "thread-3"}, set, "Resolved thread %s\n")

	if len(called) != 3 {
		t.Fatalf("set called %d times, want 3 (calls: %v)", len(called), called)
	}
	if err == nil {
		t.Fatal("expected an error for thread-2")
	}
	if !strings.Contains(err.Error(), "thread-2: not found") {
		t.Errorf("error does not name the failing thread: %v", err)
	}
	if strings.Contains(err.Error(), "thread-3") {
		t.Errorf("error reports a thread that succeeded: %v", err)
	}

	got := out.String()
	want := "Resolved thread thread-1\nResolved thread thread-3\n"
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestApplyThreadStateJoinsEveryFailure(t *testing.T) {
	var called int
	set := func(id string) error {
		called++
		return errors.New("boom")
	}

	err := applyThreadState(&bytes.Buffer{}, []string{"a", "b", "c"}, set, "%s\n")

	if called != 3 {
		t.Fatalf("set called %d times, want 3", called)
	}
	for _, id := range []string{"thread a", "thread b", "thread c"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error is missing %q: %v", id, err)
		}
	}
}

func TestApplyThreadStateReturnsNilWhenAllSucceed(t *testing.T) {
	var out bytes.Buffer
	set := func(string) error { return nil }

	if err := applyThreadState(&out, []string{"a", "b"}, set, "ok %s\n"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := out.String(), "ok a\nok b\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
