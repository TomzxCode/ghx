package github

import (
	"encoding/base64"
	"strings"
	"time"
)

// prCursorPrefix is the fixed prefix GitHub puts in front of a pull request
// connection cursor's msgpack payload.
const prCursorPrefix = "cursor:v2:"

// PRCursorAt returns the keyset cursor for the first pull request updated at or
// after t in ascending updatedAt order.
//
// GitHub documents cursors as opaque, but a pull request cursor decodes to a
// msgpack pair of [updatedAt, databaseId], and GitHub accepts a key that does
// not match an existing row. Using a databaseId of 0 sorts the key before every
// pull request updated at the same second, which makes it a lower bound.
//
// This is an implementation detail, not a contract: a rejected cursor leaves
// the coverage segment unfinished, and the next run retries it.
func PRCursorAt(t time.Time) string {
	ts := t.UTC().Truncate(time.Second).Format(time.RFC3339)
	payload := make([]byte, 0, 2+len(ts)+5)
	payload = append(payload, 0x92)               // msgpack fixarray, 2 elements
	payload = append(payload, 0xA0|byte(len(ts))) // fixstr holding the timestamp
	payload = append(payload, ts...)
	payload = append(payload, 0xCE, 0, 0, 0, 0) // uint32 databaseId 0
	return base64.StdEncoding.EncodeToString(append([]byte(prCursorPrefix), payload...))
}

// PRCursorTime returns the updatedAt stored in a pull request cursor produced by
// PRCursorAt or by GitHub. ok is false when the value is not a cursor in the
// expected shape.
func PRCursorTime(cursor string) (time.Time, bool) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil || !strings.HasPrefix(string(raw), prCursorPrefix) {
		return time.Time{}, false
	}
	b := raw[len(prCursorPrefix):]
	if len(b) < 3 || b[0] != 0x92 {
		return time.Time{}, false
	}
	tag := b[1]
	if tag < 0xA0 || tag > 0xBF {
		return time.Time{}, false
	}
	n := int(tag & 0x1F)
	if len(b) < 2+n {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, string(b[2:2+n]))
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}
