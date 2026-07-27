// Package ids generates the prefixed, sortable identifiers used throughout
// Hubchat (docs/architecture.md §6): a ULID for uniqueness and monotonic
// ordering, prefixed with a short type tag so an id is self-describing in
// logs, request traces, and support tickets about support tickets.
package ids

import (
	"crypto/rand"
	"strings"

	"github.com/oklog/ulid/v2"
)

// entropy is a dedicated reader rather than ulid.DefaultEntropy() (which is
// monotonic but not safe for concurrent use across goroutines without a
// mutex). crypto/rand is safe for concurrent use and fast enough at our
// volumes; monotonic ordering within the same millisecond is not something we
// rely on.
func newULID() ulid.ULID {
	return ulid.MustNew(ulid.Now(), rand.Reader)
}

// New returns a new id of the form "<prefix>_<ulid>", lowercased so ids read
// consistently regardless of where they are printed.
func New(prefix string) string {
	return prefix + "_" + strings.ToLower(newULID().String())
}

// Prefixes used across the schema. Keeping them named here means a module
// never hard-codes its own tag inline, and a grep finds every id of one kind.
const (
	PrefixUser         = "usr"
	PrefixSession      = "ses"
	PrefixOAuthAccount = "oau"
	PrefixEmailToken   = "evt" // email verification token
	PrefixResetToken   = "prt" // password reset token
	PrefixWorkspace    = "wrk"
	PrefixMember       = "mem"
	PrefixInvite       = "inv"
	PrefixTeam         = "tea"
	PrefixRole         = "rol"
	PrefixInbox        = "inb"
	PrefixCompany      = "cmp"
	PrefixCustomer     = "cus"
	PrefixVisitor      = "vis"
	PrefixConversation = "cnv"
	PrefixMessage      = "msg"
	PrefixTag          = "tag"
	PrefixTicket       = "tkt"
	PrefixAuditLog     = "aud"
	PrefixJob          = "job"
)
