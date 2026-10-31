package github

import (
	"testing"
	"time"
)

func TestPRCursorAt(t *testing.T) {
	got := PRCursorAt(time.Date(2026, 10, 1, 12, 34, 56, 0, time.UTC))
	want := "Y3Vyc29yOnYyOpK0MjAyNi0xMC0wMVQxMjozNDo1NlrOAAAAAA=="
	if got != want {
		t.Errorf("PRCursorAt = %q, want %q", got, want)
	}
}

func TestPRCursorAt_TruncatesToSeconds(t *testing.T) {
	withFraction := time.Date(2026, 10, 1, 12, 34, 56, 900_000_000, time.UTC)
	if got, want := PRCursorAt(withFraction), PRCursorAt(withFraction.Truncate(time.Second)); got != want {
		t.Errorf("fractional timestamp should truncate: %q != %q", got, want)
	}
}

func TestPRCursorTime(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
		want   time.Time
		ok     bool
	}{
		{
			name:   "synthetic",
			cursor: PRCursorAt(time.Date(2026, 10, 1, 12, 34, 56, 0, time.UTC)),
			want:   time.Date(2026, 10, 1, 12, 34, 56, 0, time.UTC),
			ok:     true,
		},
		{
			name:   "github uint32 id",
			cursor: "Y3Vyc29yOnYyOpK0MjAyNi0wMi0xMlQyMzo1MzowOVrOw2uVNA==",
			want:   time.Date(2026, 2, 12, 23, 53, 9, 0, time.UTC),
			ok:     true,
		},
		{
			name:   "not base64",
			cursor: "not-a-cursor",
			ok:     false,
		},
		{
			name:   "wrong prefix",
			cursor: "bm90LWEtY3Vyc29y",
			ok:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PRCursorTime(tt.cursor)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && !got.Equal(tt.want) {
				t.Errorf("time = %v, want %v", got, tt.want)
			}
		})
	}
}
