//go:build integration

package push

import (
	"bytes"
	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/hubchat/hubchat/internal/database/dbtest"
	"testing"
)

func TestSubscriptionUpdatesAndRemovalStayWithinMemberScope(t *testing.T) {
	pool := dbtest.Pool(t)
	dbtest.Reset(t, pool)
	ctx := dbtest.Context(t)
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,name,email,password_hash) VALUES('usr_push_one','One','push-one@example.test','unused'),('usr_push_two','Two','push-two@example.test','unused');INSERT INTO workspaces(id,name,slug) VALUES('wrk_push','Push','push');INSERT INTO workspace_members(id,workspace_id,user_id,role) VALUES('mem_push_one','wrk_push','usr_push_one','owner'),('mem_push_two','wrk_push','usr_push_two','agent')`); err != nil {
		t.Fatal(err)
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	service := New(pool, []byte("push-test-secret"), Options{PublicKey: public, PrivateKey: private, Subscriber: "support@example.test"})
	subscription := testSubscription(t)
	first, err := service.Subscribe(ctx, "wrk_push", "mem_push_one", subscription)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Subscribe(ctx, "wrk_push", "mem_push_one", subscription)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("duplicate browser created another subscription")
	}
	var encrypted []byte
	if err := pool.QueryRow(ctx, `SELECT subscription FROM push_subscriptions WHERE id=$1`, first).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("private-token")) {
		t.Fatal("plaintext endpoint in DB")
	}
	if err := service.UnsubscribeID(ctx, "wrk_push", "mem_push_two", first); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE id=$1`, first).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("another member deleted the subscription")
	}
	if err := service.UnsubscribeID(ctx, "wrk_push", "mem_push_one", first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE id=$1`, first).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("subscription removal failed")
	}
}
