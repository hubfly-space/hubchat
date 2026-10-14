package customer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
	"github.com/jackc/pgx/v5"
)

var ErrInvalidContextCard = errors.New("customer: a context card needs a title and one to eight declared customer attributes")

type ContextField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Format   string `json:"format,omitempty"`
	Currency string `json:"currency,omitempty"`
}
type ContextCard struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Fields    []ContextField `json:"fields"`
	CreatedAt time.Time      `json:"created_at"`
}

func (s *Service) ContextCards(ctx context.Context, workspaceID string) ([]ContextCard, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,title,fields,created_at FROM customer_context_cards WHERE workspace_id=$1 ORDER BY created_at,id LIMIT 20`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextCard{}
	for rows.Next() {
		var card ContextCard
		var fields []byte
		if err := rows.Scan(&card.ID, &card.Title, &fields, &card.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(fields, &card.Fields); err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	return out, rows.Err()
}
func (s *Service) SaveContextCard(ctx context.Context, workspaceID, id string, input ContextCard) (*ContextCard, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Title) > 100 || len(input.Fields) == 0 || len(input.Fields) > 8 {
		return nil, ErrInvalidContextCard
	}
	for _, field := range input.Fields {
		if field.Key == "" || len(field.Label) > 100 {
			return nil, ErrInvalidContextCard
		}
		switch field.Format {
		case "", "text", "date", "currency", "link":
		default:
			return nil, ErrInvalidContextCard
		}
	}
	body, err := json.Marshal(input.Fields)
	if err != nil {
		return nil, err
	}
	create := id == ""
	if create {
		id = ids.New("ccr")
	}
	var card ContextCard
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "customer:context-cards:"+workspaceID); err != nil {
			return err
		}
		for _, field := range input.Fields {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attribute_definitions WHERE workspace_id=$1 AND entity_type='customer' AND key=$2)`, workspaceID, field.Key).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrInvalidContextCard
			}
		}
		if create {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM customer_context_cards WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
				return err
			}
			if count >= 20 {
				return ErrInvalidContextCard
			}
			return tx.QueryRow(ctx, `INSERT INTO customer_context_cards(id,workspace_id,title,fields) VALUES($1,$2,$3,$4) RETURNING id,title,created_at`, id, workspaceID, input.Title, body).Scan(&card.ID, &card.Title, &card.CreatedAt)
		}
		err := tx.QueryRow(ctx, `UPDATE customer_context_cards SET title=$3,fields=$4 WHERE workspace_id=$1 AND id=$2 RETURNING id,title,created_at`, workspaceID, id, input.Title, body).Scan(&card.ID, &card.Title, &card.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	card.Fields = input.Fields
	return &card, err
}
func (s *Service) DeleteContextCard(ctx context.Context, workspaceID, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM customer_context_cards WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
