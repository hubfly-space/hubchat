package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
	"github.com/hubchat/hubchat/internal/jobs"
)

// registerOpsRoutes exposes the workspace command center. It intentionally
// reads existing durable operational tables rather than introducing a second
// health database or a polling cache.
func registerOpsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/ops/summary", requireCapability(deps, authorization.WorkspaceManage, handleOpsSummary(deps)))
	mux.HandleFunc("POST /v1/ops/test-email", requireCapability(deps, authorization.WorkspaceManage, Idempotency(deps)(handleOpsTestEmail(deps))))
}

type opsCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type opsSummary struct {
	Jobs       opsJobs     `json:"jobs"`
	Webhooks   opsWebhooks `json:"webhooks"`
	Email      opsEmail    `json:"email"`
	Storage    opsStorage  `json:"storage"`
	Realtime   opsRealtime `json:"realtime"`
	Checks     []opsCheck  `json:"checks"`
	ComputedAt time.Time   `json:"computed_at"`
}

type opsJobs struct {
	QueueDepth int `json:"queue_depth"`
	Running    int `json:"running"`
	Failed24h  int `json:"failed_24h"`
	Dead       int `json:"dead"`
}

type opsWebhooks struct {
	Pending      int `json:"pending"`
	Failed24h    int `json:"failed_24h"`
	Exhausted    int `json:"exhausted"`
	AutoDisabled int `json:"auto_disabled"`
}

type opsEmail struct {
	Mailboxes         int `json:"mailboxes"`
	DeliveryErrors24h int `json:"delivery_errors_24h"`
	Bounces24h        int `json:"bounces_24h"`
	Suppressions      int `json:"suppressions"`
}

type opsStorage struct {
	Backend        string `json:"backend"`
	CommittedFiles int    `json:"committed_files"`
	Bytes          int64  `json:"bytes"`
	PendingUploads int    `json:"pending_uploads"`
}

type opsRealtime struct {
	Connections int `json:"connections"`
}

func handleOpsSummary(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		workspaceID := actorFromRequest(r).WorkspaceID
		summary := opsSummary{ComputedAt: time.Now().UTC(), Storage: opsStorage{Backend: deps.Config.Storage.Backend}}

		jobSummary, err := deps.Jobs.Summary(r.Context(), workspaceID)
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not load operational job health.")
			return
		}
		summary.Jobs = opsJobs{QueueDepth: jobSummary.QueueDepth, Running: jobSummary.Running, Failed24h: jobSummary.Failed24h, Dead: jobSummary.Dead}

		dayAgo := time.Now().UTC().Add(-24 * time.Hour)
		if err := deps.Pool.QueryRow(r.Context(), `
			SELECT count(*) FILTER (WHERE status='pending'),
			       count(*) FILTER (WHERE status='failed' AND created_at >= $2),
			       count(*) FILTER (WHERE status='exhausted')
			FROM webhook_deliveries WHERE workspace_id=$1
		`, workspaceID, dayAgo).Scan(&summary.Webhooks.Pending, &summary.Webhooks.Failed24h, &summary.Webhooks.Exhausted); err != nil {
			writeOpsQueryError(w, r, "webhook health", err)
			return
		}
		if err := deps.Pool.QueryRow(r.Context(), `SELECT count(*) FROM webhook_endpoints WHERE workspace_id=$1 AND auto_disabled_at IS NOT NULL`, workspaceID).Scan(&summary.Webhooks.AutoDisabled); err != nil {
			writeOpsQueryError(w, r, "webhook endpoint health", err)
			return
		}
		if err := deps.Pool.QueryRow(r.Context(), `
			SELECT count(*) FILTER (WHERE event_type IN ('bounce','bounced','failed','delivery_failed') AND occurred_at >= $2),
			       count(*) FILTER (WHERE event_type IN ('bounce','bounced') AND occurred_at >= $2)
			FROM email_delivery_events WHERE workspace_id=$1
		`, workspaceID, dayAgo).Scan(&summary.Email.DeliveryErrors24h, &summary.Email.Bounces24h); err != nil {
			writeOpsQueryError(w, r, "email delivery health", err)
			return
		}
		if err := deps.Pool.QueryRow(r.Context(), `SELECT count(*) FROM email_mailboxes WHERE workspace_id=$1`, workspaceID).Scan(&summary.Email.Mailboxes); err != nil {
			writeOpsQueryError(w, r, "email mailbox health", err)
			return
		}
		if err := deps.Pool.QueryRow(r.Context(), `SELECT count(*) FROM email_suppressions WHERE workspace_id=$1`, workspaceID).Scan(&summary.Email.Suppressions); err != nil {
			writeOpsQueryError(w, r, "email suppression health", err)
			return
		}
		if err := deps.Pool.QueryRow(r.Context(), `
			SELECT count(*) FILTER (WHERE committed_at IS NOT NULL),
			       coalesce(sum(size_bytes) FILTER (WHERE committed_at IS NOT NULL),0),
			       count(*) FILTER (WHERE committed_at IS NULL)
			FROM files WHERE workspace_id=$1
		`, workspaceID).Scan(&summary.Storage.CommittedFiles, &summary.Storage.Bytes, &summary.Storage.PendingUploads); err != nil {
			writeOpsQueryError(w, r, "storage health", err)
			return
		}
		if deps.Hub != nil {
			summary.Realtime.Connections = deps.Hub.ConnectionCount()
		}
		storageReady, storageDetail := storageReadiness(deps)
		publicURL := publicURLString(deps)
		emailReady := deps.Config.Email.Enabled && strings.TrimSpace(deps.Config.Email.SMTPHost) != "" && strings.TrimSpace(deps.Config.Email.FromAddress) != ""
		var widgetCount, allowedDomainCount int
		if err := deps.Pool.QueryRow(r.Context(), `SELECT count(*) FROM widgets WHERE workspace_id=$1 AND enabled`, workspaceID).Scan(&widgetCount); err != nil {
			writeOpsQueryError(w, r, "widget health", err)
			return
		}
		if err := deps.Pool.QueryRow(r.Context(), `SELECT count(*) FROM widget_domains d JOIN widgets w ON w.id=d.widget_id WHERE w.workspace_id=$1`, workspaceID).Scan(&allowedDomainCount); err != nil {
			writeOpsQueryError(w, r, "widget domain health", err)
			return
		}
		summary.Checks = []opsCheck{
			{ID: "database", Status: "pass", Detail: "PostgreSQL responded to the operational checks."},
			{ID: "public_url", Status: checkStatus(publicURL != ""), Detail: publicURLDetail(publicURL)},
			{ID: "storage", Status: checkStatus(storageReady), Detail: storageDetail},
			{ID: "email", Status: checkWarnStatus(emailReady), Detail: opsEmailDetail(emailReady)},
			{ID: "widget", Status: widgetCheckStatus(widgetCount, allowedDomainCount), Detail: fmt.Sprintf("%d enabled widget(s), %d allowlisted domain(s).", widgetCount, allowedDomainCount)},
		}
		httpserver.WriteJSON(w, http.StatusOK, summary)
	}
}

func handleOpsTestEmail(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFromRequest(r)
		if deps.Jobs == nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "The email job queue is unavailable.")
			return
		}
		if !deps.Config.Email.Enabled || strings.TrimSpace(deps.Config.Email.SMTPHost) == "" || strings.TrimSpace(deps.Config.Email.FromAddress) == "" {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "Configure SMTP and a sender address before sending a test email.")
			return
		}
		if strings.TrimSpace(actor.UserID) == "" {
			httpserver.WriteError(w, r, http.StatusForbidden, httpserver.CodeForbidden, "A signed-in user is required to receive the test email.")
			return
		}

		var recipient, name string
		if err := deps.Pool.QueryRow(r.Context(), `SELECT email, name FROM users WHERE id=$1`, actor.UserID).Scan(&recipient, &name); err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not resolve the current user email.")
			return
		}
		if strings.TrimSpace(recipient) == "" {
			httpserver.WriteError(w, r, http.StatusUnprocessableEntity, httpserver.CodeValidationError, "The current user has no email address.")
			return
		}
		jobID, err := deps.Jobs.Enqueue(r.Context(), jobs.Spec{
			WorkspaceID: actor.WorkspaceID,
			Queue:       "email",
			Type:        JobEmailSend,
			Payload: EmailPayload{
				To:          recipient,
				Subject:     "Hubchat test email",
				Body:        fmt.Sprintf("Hi %s,\n\nThis diagnostic message confirms that Hubchat queued outbound email for workspace %s.\n\nIf it arrives, SMTP delivery is working.", strings.TrimSpace(name), actor.WorkspaceID),
				WorkspaceID: actor.WorkspaceID,
			},
		})
		if err != nil {
			httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, "Could not queue the test email.")
			return
		}
		if deps.Audit != nil {
			if auditErr := deps.Audit.Record(r.Context(), audit.Entry{
				WorkspaceID: actor.WorkspaceID,
				ActorType:   audit.ActorUser,
				ActorID:     actor.UserID,
				Action:      audit.OpsTestEmailQueued,
				EntityType:  "email",
				EntityID:    jobID,
				RequestID:   httpserver.RequestIDFrom(r.Context()),
				Metadata:    map[string]any{"recipient_domain": emailDomain(recipient)},
			}); auditErr != nil {
				deps.Logger.Warn("could not record test email audit", "error", auditErr)
			}
		}
		httpserver.WriteJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "recipient": recipient, "status": "queued"})
	}
}

func emailDomain(address string) string {
	parts := strings.SplitN(strings.TrimSpace(address), "@", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

func opsEmailDetail(ready bool) string {
	if ready {
		return "SMTP host and sender address are configured; a test message can be queued."
	}
	return "SMTP is not fully configured; outbound messages will remain queued or be skipped."
}

func widgetCheckStatus(widgets, domains int) string {
	if widgets > 0 && domains > 0 {
		return "pass"
	}
	if widgets > 0 {
		return "warn"
	}
	return "warn"
}

func checkWarnStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "warn"
}

func writeOpsQueryError(w http.ResponseWriter, r *http.Request, subject string, err error) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, fmt.Sprintf("Could not load %s.", subject))
}
