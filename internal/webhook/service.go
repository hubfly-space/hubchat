// Package webhook owns signed, durable outbound webhook delivery.
//
// Endpoint secrets are encrypted because delivery needs the original HMAC
// key. Delivery rows keep the exact event payload so retries and replays are
// deterministic even if the source entity changes later.
package webhook

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
)

const (
	JobDeliver      = "webhook.deliver"
	maxEvents       = 200
	maxResponseBody = 4096
	maxAttempts     = 6
)

var (
	ErrInvalidURL    = errors.New("webhook: URL must use http or https and include a host")
	ErrInvalidEvents = errors.New("webhook: event type is invalid")
	ErrNotFound      = errors.New("webhook: not found")
	ErrSecret        = errors.New("webhook: could not generate or decrypt secret")
)

type Service struct {
	pool   *database.Pool
	jobs   *jobs.Client
	key    []byte
	client *http.Client

	mu   sync.Mutex
	seen map[string]int64
}

type Endpoint struct {
	ID                  string     `json:"id"`
	WorkspaceID         string     `json:"workspace_id"`
	URL                 string     `json:"url"`
	Description         string     `json:"description,omitempty"`
	Events              []string   `json:"events"`
	SecretHint          string     `json:"secret_hint"`
	Enabled             bool       `json:"enabled"`
	AutoDisabledAt      *time.Time `json:"auto_disabled_at,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	CreatedBy           *string    `json:"created_by,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	Success24h          int        `json:"success_24h"`
	Failure24h          int        `json:"failure_24h"`
}

type Created struct {
	Endpoint Endpoint `json:"endpoint"`
	Secret   string   `json:"secret"`
}

type Input struct {
	URL         string
	Description string
	Events      []string
	Enabled     bool
}

type Delivery struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	EndpointID     string     `json:"endpoint_id"`
	EventID        *string    `json:"event_id,omitempty"`
	EventType      string     `json:"event_type"`
	Status         string     `json:"status"`
	Attempt        int        `json:"attempt"`
	MaxAttempts    int        `json:"max_attempts"`
	ResponseStatus *int       `json:"response_status,omitempty"`
	ResponseBody   string     `json:"response_body,omitempty"`
	DurationMS     *int       `json:"duration_ms,omitempty"`
	Error          string     `json:"error,omitempty"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type deliveryPayload struct {
	DeliveryID string `json:"delivery_id"`
}

func New(pool *database.Pool, secretKey []byte, queue *jobs.Client) *Service {
	hash := sha256.Sum256(append([]byte("hubchat/webhook-secrets:"), secretKey...))
	return &Service{
		pool: pool, jobs: queue, key: hash[:],
		client: &http.Client{Timeout: 10 * time.Second},
		seen:   make(map[string]int64),
	}
}

func (s *Service) Create(ctx context.Context, workspaceID, memberID string, input Input) (*Created, error) {
	endpointURL, err := validateURL(input.URL)
	if err != nil {
		return nil, err
	}
	eventTypes, err := normalizeEvents(input.Events)
	if err != nil {
		return nil, err
	}
	secret, err := randomSecret()
	if err != nil {
		return nil, ErrSecret
	}
	ciphertext, err := s.encrypt(secret)
	if err != nil {
		return nil, ErrSecret
	}
	id := ids.New(ids.PrefixWebhookEndpoint)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO webhook_endpoints (id, workspace_id, url, description, events, secret, secret_hint, enabled, created_by)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,NULLIF($9,''))
	`, id, workspaceID, endpointURL, strings.TrimSpace(input.Description), eventTypes, ciphertext, secretHint(secret), input.Enabled, memberID)
	if err != nil {
		return nil, fmt.Errorf("webhook: create: %w", err)
	}
	endpoint, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return &Created{Endpoint: *endpoint, Secret: secret}, nil
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Endpoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id,e.workspace_id,e.url,coalesce(e.description,''),e.events,e.secret_hint,e.enabled,
		       e.auto_disabled_at,e.consecutive_failures,e.created_by,e.created_at,e.updated_at,
		       (SELECT count(*) FROM webhook_deliveries d WHERE d.endpoint_id=e.id AND d.status='delivered' AND d.created_at >= now()-interval '24 hours'),
		       (SELECT count(*) FROM webhook_deliveries d WHERE d.endpoint_id=e.id AND d.status IN ('failed','exhausted') AND d.created_at >= now()-interval '24 hours')
		FROM webhook_endpoints e WHERE e.workspace_id=$1 ORDER BY e.created_at DESC,e.id DESC
	`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("webhook: list: %w", err)
	}
	defer rows.Close()
	result := make([]Endpoint, 0)
	for rows.Next() {
		var endpoint Endpoint
		if err := rows.Scan(&endpoint.ID, &endpoint.WorkspaceID, &endpoint.URL, &endpoint.Description, &endpoint.Events, &endpoint.SecretHint, &endpoint.Enabled, &endpoint.AutoDisabledAt, &endpoint.ConsecutiveFailures, &endpoint.CreatedBy, &endpoint.CreatedAt, &endpoint.UpdatedAt, &endpoint.Success24h, &endpoint.Failure24h); err != nil {
			return nil, fmt.Errorf("webhook: scan: %w", err)
		}
		result = append(result, endpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhook: list rows: %w", err)
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*Endpoint, error) {
	var endpoint Endpoint
	err := s.pool.QueryRow(ctx, `
		SELECT id,workspace_id,url,coalesce(description,''),events,secret_hint,enabled,auto_disabled_at,
		       consecutive_failures,created_by,created_at,updated_at
		FROM webhook_endpoints WHERE workspace_id=$1 AND id=$2
	`, workspaceID, id).Scan(&endpoint.ID, &endpoint.WorkspaceID, &endpoint.URL, &endpoint.Description, &endpoint.Events, &endpoint.SecretHint, &endpoint.Enabled, &endpoint.AutoDisabledAt, &endpoint.ConsecutiveFailures, &endpoint.CreatedBy, &endpoint.CreatedAt, &endpoint.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("webhook: get: %w", err)
	}
	return &endpoint, nil
}

func (s *Service) Update(ctx context.Context, workspaceID, id string, input Input) (*Endpoint, error) {
	endpointURL, err := validateURL(input.URL)
	if err != nil {
		return nil, err
	}
	eventTypes, err := normalizeEvents(input.Events)
	if err != nil {
		return nil, err
	}
	var endpoint Endpoint
	err = s.pool.QueryRow(ctx, `
		UPDATE webhook_endpoints SET url=$3,description=NULLIF($4,''),events=$5,enabled=$6,
			auto_disabled_at=CASE WHEN $6 THEN NULL ELSE auto_disabled_at END,
			consecutive_failures=CASE WHEN $6 THEN 0 ELSE consecutive_failures END,updated_at=now()
		WHERE workspace_id=$1 AND id=$2
		RETURNING id,workspace_id,url,coalesce(description,''),events,secret_hint,enabled,auto_disabled_at,consecutive_failures,created_by,created_at,updated_at
	`, workspaceID, id, endpointURL, strings.TrimSpace(input.Description), eventTypes, input.Enabled).Scan(&endpoint.ID, &endpoint.WorkspaceID, &endpoint.URL, &endpoint.Description, &endpoint.Events, &endpoint.SecretHint, &endpoint.Enabled, &endpoint.AutoDisabledAt, &endpoint.ConsecutiveFailures, &endpoint.CreatedBy, &endpoint.CreatedAt, &endpoint.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("webhook: update: %w", err)
	}
	return &endpoint, nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM webhook_endpoints WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("webhook: delete: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) RotateSecret(ctx context.Context, workspaceID, id string) (*Created, error) {
	secret, err := randomSecret()
	if err != nil {
		return nil, ErrSecret
	}
	ciphertext, err := s.encrypt(secret)
	if err != nil {
		return nil, ErrSecret
	}
	var endpoint Endpoint
	err = s.pool.QueryRow(ctx, `
		UPDATE webhook_endpoints SET secret=$3,secret_hint=$4,updated_at=now()
		WHERE workspace_id=$1 AND id=$2
		RETURNING id,workspace_id,url,coalesce(description,''),events,secret_hint,enabled,auto_disabled_at,consecutive_failures,created_by,created_at,updated_at
	`, workspaceID, id, ciphertext, secretHint(secret)).Scan(&endpoint.ID, &endpoint.WorkspaceID, &endpoint.URL, &endpoint.Description, &endpoint.Events, &endpoint.SecretHint, &endpoint.Enabled, &endpoint.AutoDisabledAt, &endpoint.ConsecutiveFailures, &endpoint.CreatedBy, &endpoint.CreatedAt, &endpoint.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("webhook: rotate secret: %w", err)
	}
	return &Created{Endpoint: endpoint, Secret: secret}, nil
}

// DispatchRecord fans out one committed event. A unique endpoint/event index
// makes this safe when LISTEN/NOTIFY or a worker retries the same signal.
func (s *Service) DispatchRecord(ctx context.Context, record events.Record) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("webhook: marshal event: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("webhook: dispatch begin: %w", err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM webhook_endpoints WHERE workspace_id=$1 AND enabled AND auto_disabled_at IS NULL AND (cardinality(events)=0 OR $2=ANY(events))`, record.WorkspaceID, string(record.Type))
	if err != nil {
		return fmt.Errorf("webhook: select endpoints: %w", err)
	}
	var endpointIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		endpointIDs = append(endpointIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, endpointID := range endpointIDs {
		id := ids.New(ids.PrefixWebhookDelivery)
		var deliveryID string
		err := tx.QueryRow(ctx, `
			INSERT INTO webhook_deliveries (id,workspace_id,endpoint_id,event_id,event_type,payload,max_attempts,next_attempt_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,now()) ON CONFLICT (endpoint_id,event_id) WHERE event_id IS NOT NULL DO NOTHING RETURNING id
		`, id, record.WorkspaceID, endpointID, record.ID, string(record.Type), payload, maxAttempts).Scan(&deliveryID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("webhook: insert delivery: %w", err)
		}
		if s.jobs != nil {
			if _, err := jobs.EnqueueTx(ctx, tx, jobs.Spec{WorkspaceID: record.WorkspaceID, Queue: "webhooks", Type: JobDeliver, Payload: deliveryPayload{DeliveryID: deliveryID}, MaxAttempts: maxAttempts, DedupeKey: "webhook-delivery:" + deliveryID}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
				return err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("webhook: dispatch commit: %w", err)
	}
	return nil
}

// RunEventConsumer follows committed event signals. The first signal starts
// at that event, avoiding a startup burst of historical deliveries; subsequent
// signals drain every gap. Delivery uniqueness makes restarts harmless.
func (s *Service) RunEventConsumer(ctx context.Context, signals <-chan events.Signal, source interface {
	Since(context.Context, string, int64, int) ([]events.Record, error)
}) {
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-signals:
			if !ok {
				return
			}
			s.mu.Lock()
			after, exists := s.seen[signal.WorkspaceID]
			if !exists {
				after = signal.Sequence - 1
			}
			s.mu.Unlock()
			for {
				records, err := source.Since(ctx, signal.WorkspaceID, after, maxEvents)
				if err != nil {
					break
				}
				if len(records) == 0 {
					break
				}
				dispatchFailed := false
				for _, record := range records {
					if err := s.DispatchRecord(ctx, record); err != nil {
						dispatchFailed = true
						break
					}
					after = record.Sequence
				}
				if dispatchFailed {
					break
				}
				s.mu.Lock()
				s.seen[signal.WorkspaceID] = after
				s.mu.Unlock()
				if len(records) < maxEvents {
					break
				}
			}
		}
	}
}

func (s *Service) Deliver(ctx context.Context, deliveryID string) error {
	var d Delivery
	var payload []byte
	var encrypted []byte
	var endpointURL string
	err := s.pool.QueryRow(ctx, `
		UPDATE webhook_deliveries d
		SET attempt=d.attempt+1
		FROM webhook_endpoints e
		WHERE d.id=$1 AND d.endpoint_id=e.id AND d.status IN ('pending','failed')
		RETURNING d.id,d.workspace_id,d.endpoint_id,d.event_id,d.event_type,d.payload,d.status,d.attempt,d.max_attempts,d.response_status,d.response_body,d.duration_ms,d.error,d.next_attempt_at,d.delivered_at,d.created_at,e.url,e.secret
	`, deliveryID).Scan(&d.ID, &d.WorkspaceID, &d.EndpointID, &d.EventID, &d.EventType, &payload, &d.Status, &d.Attempt, &d.MaxAttempts, &d.ResponseStatus, &d.ResponseBody, &d.DurationMS, &d.Error, &d.NextAttemptAt, &d.DeliveredAt, &d.CreatedAt, &endpointURL, &encrypted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("webhook: claim delivery: %w", err)
	}
	secret, err := s.decrypt(encrypted)
	if err != nil {
		return ErrSecret
	}
	attempt := d.Attempt
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, strings.NewReader(string(payload)))
	if err != nil {
		return s.recordFailure(ctx, deliveryID, attempt, 0, "", time.Since(started), err.Error(), d.MaxAttempts)
	}
	timestamp := time.Now()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Hubchat-Webhooks/1")
	req.Header.Set("X-Hubchat-Event", d.EventType)
	req.Header.Set("X-Hubchat-Delivery", d.ID)
	req.Header.Set("X-Hubchat-Timestamp", fmt.Sprint(timestamp.Unix()))
	req.Header.Set("X-Hubchat-Signature", Signature([]byte(secret), payload, timestamp))
	response, requestErr := s.client.Do(req)
	if requestErr != nil {
		return s.recordFailure(ctx, deliveryID, attempt, 0, "", time.Since(started), requestErr.Error(), d.MaxAttempts)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	response.Body.Close()
	if readErr != nil {
		return s.recordFailure(ctx, deliveryID, attempt, response.StatusCode, string(body), time.Since(started), readErr.Error(), d.MaxAttempts)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		_, err = s.pool.Exec(ctx, `UPDATE webhook_deliveries SET status='delivered',attempt=$2,response_status=$3,response_body=$4,duration_ms=$5,delivered_at=now(),next_attempt_at=NULL,error=NULL WHERE id=$1`, deliveryID, attempt, response.StatusCode, string(body), time.Since(started).Milliseconds())
		if err == nil {
			_, err = s.pool.Exec(ctx, `UPDATE webhook_endpoints SET consecutive_failures=0,auto_disabled_at=NULL,updated_at=now() WHERE id=(SELECT endpoint_id FROM webhook_deliveries WHERE id=$1)`, deliveryID)
		}
		return err
	}
	return s.recordFailure(ctx, deliveryID, attempt, response.StatusCode, string(body), time.Since(started), fmt.Sprintf("endpoint returned HTTP %d", response.StatusCode), d.MaxAttempts)
}

func (s *Service) recordFailure(ctx context.Context, id string, attempt, status int, body string, duration time.Duration, failure string, max int) error {
	if max <= 0 {
		max = maxAttempts
	}
	state := "failed"
	var next any = time.Now().Add(time.Duration(1<<min(attempt, 6)) * time.Second)
	if attempt >= max {
		state = "exhausted"
		next = nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE webhook_deliveries SET status=$2,attempt=$3,response_status=NULLIF($4,0),response_body=NULLIF($5,''),duration_ms=$6,error=$7,next_attempt_at=$8 WHERE id=$1`, id, state, attempt, status, body, duration.Milliseconds(), failure, next)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE webhook_endpoints SET consecutive_failures=consecutive_failures+1,auto_disabled_at=CASE WHEN consecutive_failures+1 >= 6 THEN coalesce(auto_disabled_at,now()) ELSE auto_disabled_at END,updated_at=now() WHERE id=(SELECT endpoint_id FROM webhook_deliveries WHERE id=$1)`, id)
	if err != nil {
		return err
	}
	if state == "exhausted" {
		return nil
	}
	return errors.New(failure)
}

func (s *Service) Deliveries(ctx context.Context, workspaceID, endpointID string, before time.Time, beforeID string, limit int) ([]Delivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT d.id,d.workspace_id,d.endpoint_id,d.event_id,d.event_type,d.status,d.attempt,d.max_attempts,d.response_status,d.response_body,d.duration_ms,d.error,d.next_attempt_at,d.delivered_at,d.created_at FROM webhook_deliveries d WHERE d.workspace_id=$1 AND d.endpoint_id=$2 AND ($3::timestamptz IS NULL OR (d.created_at,d.id)<($3,$4)) ORDER BY d.created_at DESC,d.id DESC LIMIT $5`, workspaceID, endpointID, nullableTime(before), beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("webhook: deliveries: %w", err)
	}
	defer rows.Close()
	result := make([]Delivery, 0)
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.WorkspaceID, &d.EndpointID, &d.EventID, &d.EventType, &d.Status, &d.Attempt, &d.MaxAttempts, &d.ResponseStatus, &d.ResponseBody, &d.DurationMS, &d.Error, &d.NextAttemptAt, &d.DeliveredAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *Service) Replay(ctx context.Context, workspaceID, endpointID, deliveryID string) (*Delivery, error) {
	var d Delivery
	var payload []byte
	err := s.pool.QueryRow(ctx, `SELECT id,event_type,payload FROM webhook_deliveries WHERE workspace_id=$1 AND endpoint_id=$2 AND id=$3`, workspaceID, endpointID, deliveryID).Scan(&d.ID, &d.EventType, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	id := ids.New(ids.PrefixWebhookDelivery)
	err = s.pool.QueryRow(ctx, `INSERT INTO webhook_deliveries(id,workspace_id,endpoint_id,event_type,payload,max_attempts,next_attempt_at) VALUES($1,$2,$3,$4,$5,$6,now()) RETURNING id,workspace_id,endpoint_id,event_type,status,attempt,max_attempts,next_attempt_at,created_at`, id, workspaceID, endpointID, d.EventType, payload, maxAttempts).Scan(&d.ID, &d.WorkspaceID, &d.EndpointID, &d.EventType, &d.Status, &d.Attempt, &d.MaxAttempts, &d.NextAttemptAt, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	if s.jobs != nil {
		_, err = s.jobs.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Queue: "webhooks", Type: JobDeliver, Payload: deliveryPayload{DeliveryID: id}, MaxAttempts: maxAttempts, DedupeKey: "webhook-delivery:" + id})
	}
	return &d, err
}

func (s *Service) Test(ctx context.Context, workspaceID, endpointID string) (*Delivery, error) {
	var d Delivery
	payload := []byte(`{"type":"webhook.test","data":{"message":"Hubchat webhook test"}}`)
	id := ids.New(ids.PrefixWebhookDelivery)
	err := s.pool.QueryRow(ctx, `INSERT INTO webhook_deliveries(id,workspace_id,endpoint_id,event_type,payload,max_attempts,next_attempt_at) VALUES($1,$2,$3,'webhook.test',$4,$5,now()) RETURNING id,workspace_id,endpoint_id,event_type,status,attempt,max_attempts,next_attempt_at,created_at`, id, workspaceID, endpointID, payload, maxAttempts).Scan(&d.ID, &d.WorkspaceID, &d.EndpointID, &d.EventType, &d.Status, &d.Attempt, &d.MaxAttempts, &d.NextAttemptAt, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	if s.jobs != nil {
		_, err = s.jobs.Enqueue(ctx, jobs.Spec{WorkspaceID: workspaceID, Queue: "webhooks", Type: JobDeliver, Payload: deliveryPayload{DeliveryID: id}, MaxAttempts: maxAttempts, DedupeKey: "webhook-delivery:" + id})
	}
	return &d, err
}

func (s *Service) encrypt(plain string) ([]byte, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, []byte(plain), nil), nil
}
func (s *Service) decrypt(ciphertext []byte) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(ciphertext) < gcm.NonceSize() {
		return "", ErrSecret
	}
	nonce := ciphertext[:gcm.NonceSize()]
	plain, err := gcm.Open(nil, nonce, ciphertext[gcm.NonceSize():], nil)
	return string(plain), err
}
func validateURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", ErrInvalidURL
	}
	return parsed.String(), nil
}
func normalizeEvents(input []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(input))
	for _, eventType := range input {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		if !strings.Contains(eventType, ".") || len(eventType) > 120 {
			return nil, ErrInvalidEvents
		}
		if !seen[eventType] {
			seen[eventType] = true
			out = append(out, eventType)
		}
	}
	return out, nil
}
func randomSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
func secretHint(secret string) string {
	if len(secret) < 4 {
		return secret
	}
	return secret[len(secret)-4:]
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
