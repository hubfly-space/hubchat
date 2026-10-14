package events

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// CompleteActionTx commits the receipt with a non-idempotent domain effect.
func CompleteActionTx(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	key := ActionKey(ctx)
	if key == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO automation_action_receipts(workspace_id,action_key) VALUES($1,$2) ON CONFLICT DO NOTHING`, workspaceID, key)
	return err
}
