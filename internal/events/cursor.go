package events

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// cursorPrefix tags opaque feed cursor tokens so callers (and sol itself)
// can tell a cursor apart from the --since duration form ("1h", "30m")
// without attempting to parse it as either. The prefix and the encoding
// behind it are NOT a public contract — consumers must treat the whole
// token as opaque and pass it back verbatim. Both may change between sol
// versions.
const cursorPrefix = "sc1:"

// ErrInvalidCursor is returned when a --since cursor cannot be decoded, or
// when the event it references can no longer be located in the feed being
// read (including the case where chronicle has rotated that event out of
// retention). There is no partial-recovery path from this state: callers
// should surface a clear "restart from a fresh cursor" instruction — see
// docs/decisions/0043-external-automation-contract.md decision 2.
var ErrInvalidCursor = errors.New("invalid or expired feed cursor")

// Cursor identifies a position in an event feed: the last event a consumer
// has already seen. Encode/decode via EncodeCursor/DecodeCursor only — the
// zero value is not a valid wire token.
type Cursor struct {
	ID       string // EventID of the last-seen event
	UnixNano int64  // that event's timestamp (UnixNano) — used to tell "not found because rotated away" from "not found because malformed"
}

// cursorWire is the JSON shape embedded in the opaque cursor token. Kept
// separate from Cursor so field names on the wire stay short without
// cramping the Go-side API.
type cursorWire struct {
	ID string `json:"i"`
	TS int64  `json:"t"`
}

// EncodeCursor serializes a Cursor to its opaque wire form.
func EncodeCursor(c Cursor) string {
	data, err := json.Marshal(cursorWire{ID: c.ID, TS: c.UnixNano})
	if err != nil {
		// c's fields are a string and an int64 — Marshal cannot fail here.
		panic(fmt.Sprintf("events: cursor marshal unexpectedly failed: %v", err))
	}
	return cursorPrefix + base64.RawURLEncoding.EncodeToString(data)
}

// DecodeCursor parses an opaque cursor token produced by EncodeCursor.
// Returns an error wrapping ErrInvalidCursor if the token is malformed.
func DecodeCursor(s string) (Cursor, error) {
	if !IsCursor(s) {
		return Cursor{}, fmt.Errorf("%w: not a recognized cursor token", ErrInvalidCursor)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, cursorPrefix))
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	var w cursorWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if w.ID == "" {
		return Cursor{}, fmt.Errorf("%w: empty event id", ErrInvalidCursor)
	}
	return Cursor{ID: w.ID, UnixNano: w.TS}, nil
}

// IsCursor reports whether s looks like an opaque cursor token produced by
// EncodeCursor, as opposed to a --since duration like "1h".
func IsCursor(s string) bool {
	return strings.HasPrefix(s, cursorPrefix)
}

// EventID computes a stable identifier for an event from its own content
// (timestamp + source + type + actor + visibility + payload) rather than a
// persisted sequence counter. This is what lets cursor-based reads work
// without any change to how events are written or where: the same event
// hashes to the same id whether it's read from the raw feed or the curated
// feed, on this process or another one, today or after a rotation moved it
// within the file. Nanosecond timestamp precision plus full content in the
// hash makes collisions astronomically unlikely, but EventID is a content
// fingerprint, not a guaranteed-unique sequence number.
func EventID(ev Event) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%s\x00%s\x00%s\x00%s\x00", ev.Timestamp.UnixNano(), ev.Source, ev.Type, ev.Actor, ev.Visibility)
	if payload, err := json.Marshal(ev.Payload); err == nil {
		h.Write(payload)
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}
