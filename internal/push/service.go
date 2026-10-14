// Package push owns opt-in, encrypted Web Push subscriptions and durable delivery.
package push

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/hubchat/hubchat/internal/jobs"
	"github.com/hubchat/hubchat/internal/webhook"
	"github.com/jackc/pgx/v5"
)

const JobDeliver = "push.deliver"

var ErrInvalidSubscription = errors.New("push: invalid subscription")
var ErrDisabled = errors.New("push: deployment has not configured VAPID keys")

type Options struct {
	PublicKey, PrivateKey, Subscriber string
	Authorize                         func(context.Context, string, string, string, string) bool
}
type Service struct {
	pool    *database.Pool
	options Options
	key     [32]byte
	client  *http.Client
}
type Delivery struct {
	SubscriptionID string `json:"subscription_id"`
	NotificationID string `json:"notification_id"`
}

func New(pool *database.Pool, secret []byte, options Options) *Service {
	return &Service{pool: pool, options: options, key: sha256.Sum256(append([]byte("hubchat:push:"), secret...)), client: webhook.NewHTTPClient(nil)}
}
func (s *Service) Enabled() bool {
	return s != nil && s.options.PublicKey != "" && s.options.PrivateKey != ""
}
func (s *Service) PublicKey() string {
	if !s.Enabled() {
		return ""
	}
	return s.options.PublicKey
}
func validate(subscription webpush.Subscription) error {
	parsed, err := url.Parse(subscription.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || len(subscription.Endpoint) > 4096 {
		return ErrInvalidSubscription
	}
	auth, err := base64.RawURLEncoding.DecodeString(subscription.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return ErrInvalidSubscription
	}
	key, err := base64.RawURLEncoding.DecodeString(subscription.Keys.P256dh)
	if err != nil {
		return ErrInvalidSubscription
	}
	if _, err := ecdh.P256().NewPublicKey(key); err != nil {
		return ErrInvalidSubscription
	}
	return nil
}
func endpointHash(endpoint string) string {
	hash := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(hash[:])
}
func (s *Service) encrypt(body []byte) ([]byte, error) {
	block, _ := aes.NewCipher(s.key[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, body, nil), nil
}
func (s *Service) decrypt(body []byte) ([]byte, error) {
	block, _ := aes.NewCipher(s.key[:])
	gcm, _ := cipher.NewGCM(block)
	if len(body) < gcm.NonceSize() {
		return nil, ErrInvalidSubscription
	}
	return gcm.Open(nil, body[:gcm.NonceSize()], body[gcm.NonceSize():], nil)
}
func (s *Service) Subscribe(ctx context.Context, workspaceID, memberID string, subscription webpush.Subscription) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	if err := validate(subscription); err != nil {
		return "", err
	}
	body, _ := json.Marshal(subscription)
	encrypted, err := s.encrypt(body)
	if err != nil {
		return "", err
	}
	var id string
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "push:"+workspaceID+":"+memberID); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE workspace_id=$1 AND member_id=$2 AND endpoint_hash<>$3`, workspaceID, memberID, endpointHash(subscription.Endpoint)).Scan(&count); err != nil {
			return err
		}
		if count >= 5 {
			return errors.New("push: at most five devices per member")
		}
		return tx.QueryRow(ctx, `INSERT INTO push_subscriptions(id,workspace_id,member_id,endpoint_hash,subscription) SELECT $1,$2,$3,$4,$5 FROM workspace_members WHERE workspace_id=$2 AND id=$3 AND deactivated_at IS NULL ON CONFLICT(workspace_id,member_id,endpoint_hash) DO UPDATE SET subscription=EXCLUDED.subscription RETURNING id`, ids.New("pus"), workspaceID, memberID, endpointHash(subscription.Endpoint), encrypted).Scan(&id)
	})
	return id, err
}
func (s *Service) Unsubscribe(ctx context.Context, workspaceID, memberID, endpoint string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE workspace_id=$1 AND member_id=$2 AND endpoint_hash=$3`, workspaceID, memberID, endpointHash(endpoint))
	return err
}
func (s *Service) QueueTx(ctx context.Context, tx pgx.Tx, workspaceID, memberID, notificationID string) error {
	if !s.Enabled() {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT p.id FROM push_subscriptions p JOIN notifications n ON n.workspace_id=p.workspace_id AND n.member_id=p.member_id JOIN notification_preferences preferences ON preferences.workspace_id=p.workspace_id AND preferences.member_id=p.member_id AND preferences.type=CASE WHEN n.type='customer_reply' THEN 'reply' ELSE n.type END WHERE p.workspace_id=$1 AND p.member_id=$2 AND n.id=$3 AND preferences.browser`, workspaceID, memberID, notificationID)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := jobs.EnqueueTx(ctx, tx, jobs.Spec{WorkspaceID: workspaceID, Queue: "notifications", Type: JobDeliver, Payload: Delivery{SubscriptionID: id, NotificationID: notificationID}, DedupeKey: "push:" + id + ":" + notificationID}); err != nil && !errors.Is(err, jobs.ErrDuplicate) {
			return err
		}
	}
	return nil
}
func (s *Service) Deliver(ctx context.Context, delivery Delivery) error {
	if !s.Enabled() {
		return nil
	}
	var encrypted []byte
	var workspaceID, memberID, entityType, entityID, target string
	err := s.pool.QueryRow(ctx, `SELECT p.subscription,p.workspace_id,p.member_id,n.entity_type,n.entity_id,n.url FROM push_subscriptions p JOIN notifications n ON n.workspace_id=p.workspace_id AND n.member_id=p.member_id JOIN workspace_members m ON m.workspace_id=p.workspace_id AND m.id=p.member_id AND m.deactivated_at IS NULL WHERE p.id=$1 AND n.id=$2 AND n.read_at IS NULL`, delivery.SubscriptionID, delivery.NotificationID).Scan(&encrypted, &workspaceID, &memberID, &entityType, &entityID, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.options.Authorize == nil || !s.options.Authorize(ctx, workspaceID, memberID, entityType, entityID) {
		return nil
	}
	raw, err := s.decrypt(encrypted)
	if err != nil {
		return err
	}
	var subscription webpush.Subscription
	if err := json.Unmarshal(raw, &subscription); err != nil {
		return err
	}
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		target = "/inbox"
	}
	targetURL, _ := url.Parse("/app" + target)
	query := targetURL.Query()
	query.Set("workspace_id", workspaceID)
	targetURL.RawQuery = query.Encode()
	body, _ := json.Marshal(map[string]string{"title": "Hubchat", "body": "You have a new notification.", "url": targetURL.String(), "tag": delivery.NotificationID})
	response, err := webpush.SendNotificationWithContext(ctx, body, &subscription, &webpush.Options{Subscriber: s.options.Subscriber, VAPIDPublicKey: s.options.PublicKey, VAPIDPrivateKey: s.options.PrivateKey, TTL: 3600, HTTPClient: s.client})
	if err != nil {
		var requestErr *url.Error
		if errors.As(err, &requestErr) {
			return fmt.Errorf("push: provider request failed: %w", requestErr.Err)
		}
		return errors.New("push: encryption or authorization failed")
	}
	defer response.Body.Close()
	if response.StatusCode == 404 || response.StatusCode == 410 {
		_, err := s.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id=$1`, delivery.SubscriptionID)
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("push: provider returned %d", response.StatusCode)
	}
	return nil
}

func (s *Service) UnsubscribeID(ctx context.Context, workspaceID, memberID, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE workspace_id=$1 AND member_id=$2 AND id=$3`, workspaceID, memberID, id)
	return err
}
