package cache

import (
	"sort"
	"time"
)

// Segment is a half-open interval of item updatedAt values that the cache has
// processed. Start is inclusive, End is exclusive, and both are UTC with second
// precision.
type Segment struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// TruncateSegment normalizes a segment to UTC whole seconds. It reports false
// when the segment is empty.
func TruncateSegment(s Segment) (Segment, bool) {
	start := s.Start.UTC().Truncate(time.Second)
	end := s.End.UTC().Truncate(time.Second)
	if !end.After(start) {
		return Segment{}, false
	}
	return Segment{Start: start, End: end}, true
}

// MergeSegments sorts and merges overlapping or adjacent half-open segments.
// Adjacency is inclusive: [A,B) and [B,C) merge to [A,C).
func MergeSegments(in []Segment) []Segment {
	norm := make([]Segment, 0, len(in))
	for _, s := range in {
		if t, ok := TruncateSegment(s); ok {
			norm = append(norm, t)
		}
	}
	if len(norm) == 0 {
		return nil
	}
	sort.Slice(norm, func(i, j int) bool {
		if norm[i].Start.Equal(norm[j].Start) {
			return norm[i].End.Before(norm[j].End)
		}
		return norm[i].Start.Before(norm[j].Start)
	})
	out := []Segment{norm[0]}
	for _, s := range norm[1:] {
		last := &out[len(out)-1]
		if !s.Start.After(last.End) {
			if s.End.After(last.End) {
				last.End = s.End
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

// Uncovered returns the gaps in [from, to) that the merged segments do not
// cover. The input is merged defensively, so callers may pass unsorted data.
func Uncovered(segments []Segment, from, to time.Time) []Segment {
	from = from.UTC().Truncate(time.Second)
	to = to.UTC().Truncate(time.Second)
	if !to.After(from) {
		return nil
	}
	var gaps []Segment
	cur := from
	for _, s := range MergeSegments(segments) {
		if !s.End.After(cur) {
			continue
		}
		if s.Start.After(cur) {
			gaps = append(gaps, Segment{Start: cur, End: s.Start})
		}
		if s.End.After(cur) {
			cur = s.End
		}
		if !cur.Before(to) {
			break
		}
	}
	if cur.Before(to) {
		gaps = append(gaps, Segment{Start: cur, End: to})
	}
	return gaps
}

// Chunk splits segments into pieces no longer than size. A non-positive size
// returns the input unchanged.
func Chunk(segments []Segment, size time.Duration) []Segment {
	if size <= 0 {
		return segments
	}
	size = size.Truncate(time.Second)
	if size <= 0 {
		return segments
	}
	var out []Segment
	for _, s := range segments {
		for start := s.Start; start.Before(s.End); start = start.Add(size) {
			end := start.Add(size)
			if end.After(s.End) {
				end = s.End
			}
			out = append(out, Segment{Start: start, End: end})
		}
	}
	return out
}

// Covers reports whether the segments cover [from, to).
func Covers(segments []Segment, from, to time.Time) bool {
	return len(Uncovered(segments, from, to)) == 0
}
