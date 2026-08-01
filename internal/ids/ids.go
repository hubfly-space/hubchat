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
//
// Prefixes are part of the public API surface wherever the id is: §16 shows
// the event envelope carrying "evt_…", so that tag belongs to the event log
// and nothing else may take it. Internal-only ids (tokens, revisions, links)
// are free to use whatever reads clearly, because no client ever sees them.
const (
	// Accounts and tenancy.
	PrefixUser          = "usr"
	PrefixSession       = "ses"
	PrefixOAuthAccount  = "oau"
	PrefixTrustedDevice = "tdv"
	// Email verification tokens are never exposed; "evt" is the event log's.
	PrefixEmailToken = "emv"
	PrefixResetToken = "prt"
	PrefixWorkspace  = "wrk"
	PrefixMember     = "mem"
	PrefixInvite     = "inv"
	PrefixTeam       = "tea"
	PrefixRole       = "rol"

	// Support operations.
	PrefixInbox            = "inb"
	PrefixCompany          = "cmp"
	PrefixCustomer         = "cus"
	PrefixVisitor          = "vis"
	PrefixVisitorLink      = "vcl"
	PrefixConversation     = "cnv"
	PrefixConversationLink = "cnl"
	PrefixMessage          = "msg"
	PrefixMessageRevision  = "mrv"
	PrefixStatusHistory    = "sth"
	PrefixTag              = "tag"
	PrefixTicket           = "tkt"
	PrefixTicketLink       = "tkl"
	PrefixSavedView        = "svw"
	PrefixMacro            = "mac"
	PrefixSavedReply       = "rep"
	PrefixFieldDefinition  = "fld"

	// Customer context.
	PrefixCustomerEmail  = "cem"
	PrefixCustomerPhone  = "cph"
	PrefixContactSession = "cse"
	PrefixCustomerEvent  = "cev"
	PrefixCustomerNote   = "cnt"
	PrefixAttributeDef   = "atr"
	PrefixMerge          = "mrg"
	PrefixBlockedContact = "blk"

	// Customer-facing surfaces.
	PrefixWidget        = "wgt"
	PrefixWidgetVersion = "wgv"
	PrefixWidgetDomain  = "wgd"
	PrefixPortal        = "prl"
	PrefixPortalDomain  = "pdm"
	PrefixPortalNavItem = "pnv"
	PrefixPortalIdent   = "pid"
	PrefixPortalSession = "pss"
	PrefixPortalToken   = "ptk"
	PrefixAnnouncement  = "ann"
	PrefixForm          = "frm"
	PrefixFormField     = "ffl"
	PrefixSubmission    = "sub"

	// Feedback and content.
	PrefixFeedbackBoard   = "fbd"
	PrefixFeedbackItem    = "fbi"
	PrefixFeedbackComment = "fbc"
	PrefixFeedbackLink    = "fbl"
	PrefixKnowledgeBase   = "knb"
	PrefixCollection      = "col"
	PrefixArticle         = "art"
	PrefixArticleRevision = "arv"
	PrefixArticleFeedback = "afb"
	PrefixArticleSearch   = "ase"
	PrefixChangelogEntry  = "chg"
	PrefixSurvey          = "svy"
	PrefixSurveyQuestion  = "sqn"
	PrefixSurveyResponse  = "srp"

	// Automation and SLA.
	PrefixAutomationRule      = "rul"
	PrefixAutomationVersion   = "rlv"
	PrefixAutomationExecution = "rex"
	PrefixCalendar            = "cal"
	PrefixHoliday             = "hol"
	PrefixSLAPolicy           = "sla"
	PrefixSLATarget           = "slt"
	PrefixSLAInstance         = "sli"
	PrefixScheduledAction     = "sch"
	PrefixTask                = "tsk"

	// Integrations and operations.
	PrefixAPIKey          = "key"
	PrefixWebhookEndpoint = "whk"
	PrefixWebhookDelivery = "whd"
	PrefixIntegration     = "itg"
	PrefixMailbox         = "mbx"
	PrefixEmailMessage    = "eml"
	PrefixEmailDelivery   = "edl"
	PrefixFile            = "fil"
	PrefixJob             = "job"
	PrefixJobAttempt      = "jat"
	PrefixNotification    = "ntf"
	PrefixAuditLog        = "aud"
	PrefixIdempotency     = "idm"
	PrefixEvent           = "evt"
	PrefixRollup          = "rlp"
	PrefixSavedReport     = "rpt"
	PrefixReportSchedule  = "rps"
	PrefixFeatureFlag     = "flg"
	PrefixExport          = "exp"
	PrefixImport          = "imp"
	PrefixLegalHold       = "lh"
)
