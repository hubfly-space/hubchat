package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/hubchat/hubchat/internal/authorization"
	"github.com/hubchat/hubchat/internal/httpserver"
)

// registerOpsRoutes exposes the workspace command center. It intentionally
// reads existing durable operational tables rather than introducing a second
// health database or a polling cache.
func registerOpsRoutes(mux *http.ServeMux, deps Deps) {
	mux.HandleFunc("GET /v1/ops/summary", requireCapability(deps, authorization.WorkspaceManage, handleOpsSummary(deps)))
}

type opsSummary struct {
	Jobs       opsJobs     `json:"jobs"`
	Webhooks   opsWebhooks `json:"webhooks"`
	Email      opsEmail    `json:"email"`
	Storage    opsStorage  `json:"storage"`
	Realtime   opsRealtime `json:"realtime"`
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
		httpserver.WriteJSON(w, http.StatusOK, summary)
	}
}

func writeOpsQueryError(w http.ResponseWriter, r *http.Request, subject string, err error) {
	httpserver.WriteError(w, r, http.StatusInternalServerError, httpserver.CodeInternalError, fmt.Sprintf("Could not load %s.", subject))
}
