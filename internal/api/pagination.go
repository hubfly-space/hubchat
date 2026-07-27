package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// Page is the envelope every list endpoint returns (§16 cursor pagination).
//
// The shape matches the browser's `Paginated<T>` exactly, so a page of any
// resource decodes with one generic type on the client.
type Page[T any] struct {
	Data []T `json:"data"`
	// Always emitted, null when there is no next page. The browser contract
	// types this as `string | null`, and omitting the field would make it
	// `undefined` there instead — a difference a client has to special-case
	// for no reason.
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// NewPage builds a page from one extra row.
//
// Callers query for limit+1 rows: if the extra one came back there is another
// page, and it is dropped from the response. That is one query rather than a
// separate count, and it never lies the way a cached total does.
func NewPage[T any](rows []T, limit int, cursorFor func(T) Cursor) Page[T] {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	page := Page[T]{Data: rows, HasMore: hasMore}
	if page.Data == nil {
		// An empty list, never null: a client should be able to map over the
		// result without a nil check.
		page.Data = []T{}
	}
	if hasMore && len(rows) > 0 {
		encoded := cursorFor(rows[len(rows)-1]).Encode()
		page.NextCursor = &encoded
	}
	return page
}

// Cursor is an opaque position in a result set.
//
// It is opaque by contract, not merely by encoding: §16 says clients must not
// parse identifiers, and the same applies here. Base64 of a small JSON object
// keeps it debuggable for us while making it obvious to a caller that
// constructing one by hand is not supported.
//
// Both fields are carried because the sort keys in this system are timestamps
// with an id tiebreak — two conversations can share a `last_message_at` to the
// microsecond, and paging on the timestamp alone would skip or repeat one.
type Cursor struct {
	// At is the sort timestamp of the last row on the previous page.
	At time.Time `json:"at"`
	// ID breaks ties at the same timestamp.
	ID string `json:"id"`
}

// ErrBadCursor is returned when a cursor cannot be decoded.
var ErrBadCursor = errors.New("api: malformed cursor")

// Encode renders the cursor for the wire.
func (c Cursor) Encode() string {
	payload, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

// DecodeCursor parses a cursor produced by Encode. An empty string is the
// first page, which is not an error.
func DecodeCursor(encoded string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}

	var cursor Cursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return Cursor{}, ErrBadCursor
	}
	return cursor, nil
}

// IsZero reports whether this is the first page.
func (c Cursor) IsZero() bool { return c.ID == "" && c.At.IsZero() }

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// PageParams reads `limit` and `cursor` from the query string.
//
// An out-of-range limit is clamped rather than rejected: a client asking for
// ten thousand rows has made a judgement call about its own memory, not an
// error, and silently giving it the maximum is friendlier than a 400 it has to
// special-case. A malformed cursor *is* an error, because continuing from an
// unparseable position would silently return the first page again and look
// like an infinite loop to the caller.
func PageParams(r *http.Request) (limit int, cursor Cursor, err error) {
	limit = defaultPageLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil {
			return 0, Cursor{}, ErrBadCursor
		}
		limit = parsed
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}

	cursor, err = DecodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return 0, Cursor{}, err
	}

	return limit, cursor, nil
}
