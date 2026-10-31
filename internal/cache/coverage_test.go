package cache

import (
	"testing"
	"time"
)

func ts(day int) time.Time {
	return time.Date(2026, 8, day, 0, 0, 0, 0, time.UTC)
}

func TestMergeSegments(t *testing.T) {
	tests := []struct {
		name string
		in   []Segment
		want []Segment
	}{
		{
			name: "empty",
			in:   nil,
			want: nil,
		},
		{
			name: "adjacent merge",
			in:   []Segment{{ts(1), ts(2)}, {ts(2), ts(3)}},
			want: []Segment{{ts(1), ts(3)}},
		},
		{
			name: "overlap merge",
			in:   []Segment{{ts(1), ts(4)}, {ts(2), ts(3)}},
			want: []Segment{{ts(1), ts(4)}},
		},
		{
			name: "disjoint stay separate",
			in:   []Segment{{ts(1), ts(2)}, {ts(3), ts(4)}},
			want: []Segment{{ts(1), ts(2)}, {ts(3), ts(4)}},
		},
		{
			name: "unsorted and nested",
			in:   []Segment{{ts(5), ts(6)}, {ts(1), ts(3)}, {ts(2), ts(4)}},
			want: []Segment{{ts(1), ts(4)}, {ts(5), ts(6)}},
		},
		{
			name: "empty interval dropped",
			in:   []Segment{{ts(2), ts(2)}, {ts(1), ts(3)}},
			want: []Segment{{ts(1), ts(3)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeSegments(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("MergeSegments = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if !got[i].Start.Equal(tt.want[i].Start) || !got[i].End.Equal(tt.want[i].End) {
					t.Errorf("segment %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestUncovered(t *testing.T) {
	tests := []struct {
		name     string
		segments []Segment
		from, to time.Time
		want     []Segment
	}{
		{
			name: "no coverage",
			from: ts(1), to: ts(5),
			want: []Segment{{ts(1), ts(5)}},
		},
		{
			name:     "full coverage",
			segments: []Segment{{ts(1), ts(5)}},
			from:     ts(2), to: ts(4),
			want: nil,
		},
		{
			name:     "gap before and after",
			segments: []Segment{{ts(2), ts(4)}},
			from:     ts(1), to: ts(5),
			want: []Segment{{ts(1), ts(2)}, {ts(4), ts(5)}},
		},
		{
			name:     "coverage starts later",
			segments: []Segment{{ts(3), ts(5)}},
			from:     ts(1), to: ts(5),
			want: []Segment{{ts(1), ts(3)}},
		},
		{
			name:     "coverage extends past target",
			segments: []Segment{{ts(1), ts(9)}},
			from:     ts(3), to: ts(5),
			want: nil,
		},
		{
			name:     "empty target",
			segments: nil,
			from:     ts(3), to: ts(3),
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Uncovered(tt.segments, tt.from, tt.to)
			if len(got) != len(tt.want) {
				t.Fatalf("Uncovered = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if !got[i].Start.Equal(tt.want[i].Start) || !got[i].End.Equal(tt.want[i].End) {
					t.Errorf("gap %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestChunk(t *testing.T) {
	got := Chunk([]Segment{{ts(1), ts(5)}}, 24*time.Hour)
	if len(got) != 4 {
		t.Fatalf("Chunk = %+v, want 4 daily segments", got)
	}
	if !got[0].Start.Equal(ts(1)) || !got[3].End.Equal(ts(5)) {
		t.Errorf("chunk bounds wrong: %+v", got)
	}
	for i := 1; i < len(got); i++ {
		if !got[i].Start.Equal(got[i-1].End) {
			t.Errorf("chunks are not contiguous at %d: %+v", i, got)
		}
	}
}

func TestCovers(t *testing.T) {
	segments := []Segment{{ts(1), ts(3)}, {ts(3), ts(5)}}
	if !Covers(segments, ts(2), ts(4)) {
		t.Error("adjacent segments should cover the range")
	}
	if Covers(segments, ts(1), ts(6)) {
		t.Error("range past the coverage should not be covered")
	}
}
