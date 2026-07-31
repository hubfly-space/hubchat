// Package feedback owns boards, roadmap items, votes, comments, and status
// history. Counters are updated in the same transactions as their source rows
// so public sorting never depends on eventually refreshed browser state.
package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/audit"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/events"
	"github.com/hubchat/hubchat/internal/ids"
)

var (
	ErrNotFound         = errors.New("feedback: not found")
	ErrInvalidName      = errors.New("feedback: name and title are required")
	ErrInvalidSlug      = errors.New("feedback: slug is required")
	ErrInvalidStatus    = errors.New("feedback: invalid status")
	ErrInvalidType      = errors.New("feedback: invalid item type")
	ErrVotingDisabled   = errors.New("feedback: voting is disabled")
	ErrAlreadyVoted     = errors.New("feedback: customer has already voted for this item")
	ErrCustomerRequired = errors.New("feedback: customer authentication is required")
	ErrVoteLimit        = errors.New("feedback: customer vote limit reached")
	ErrCommentsDisabled = errors.New("feedback: comments are disabled")
	ErrInvalidComment   = errors.New("feedback: comment must not be empty")
	ErrInvalidMerge     = errors.New("feedback: items cannot be merged into themselves")
)

var statuses = map[string]bool{"open": true, "reviewing": true, "planned": true, "in_progress": true, "completed": true, "declined": true, "held": true}
var itemTypes = map[string]bool{"feature_request": true, "idea": true, "usability_issue": true, "bug": true, "integration_request": true, "suggestion": true, "custom": true}

type Service struct {
	pool   *database.Pool
	events *events.Log
	audit  *audit.Log
}

type Board struct {
	ID               string    `json:"id"`
	WorkspaceID      string    `json:"workspace_id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	Description      string    `json:"description,omitempty"`
	Visibility       string    `json:"visibility"`
	AllowComments    bool      `json:"allow_comments"`
	AllowVoting      bool      `json:"allow_voting"`
	VotesPerCustomer *int      `json:"votes_per_customer,omitempty"`
	Moderation       string    `json:"moderation"`
	ItemCount        int       `json:"item_count"`
	Position         int       `json:"position"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type Item struct {
	ID               string    `json:"id"`
	WorkspaceID      string    `json:"workspace_id"`
	BoardID          string    `json:"board_id"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Type             string    `json:"type"`
	Status           string    `json:"status"`
	Visibility       string    `json:"visibility"`
	SubmitterID      *string   `json:"submitter_id,omitempty"`
	CompanyID        *string   `json:"company_id,omitempty"`
	ProductArea      *string   `json:"product_area,omitempty"`
	Priority         *string   `json:"priority,omitempty"`
	VoteCount        int       `json:"vote_count"`
	CommentCount     int       `json:"comment_count"`
	SubscriberCount  int       `json:"subscriber_count"`
	ViewerHasVoted   bool      `json:"viewer_has_voted"`
	ViewerSubscribed bool      `json:"viewer_subscribed"`
	MergedIntoID     *string   `json:"merged_into_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type Comment struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	ItemID         string    `json:"item_id"`
	AuthorType     string    `json:"author_type"`
	AuthorID       *string   `json:"author_id,omitempty"`
	AuthorName     string    `json:"author_name"`
	Body           string    `json:"body"`
	OfficialUpdate bool      `json:"is_official_update"`
	CreatedAt      time.Time `json:"created_at"`
}
type BoardInput struct {
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	Description      string `json:"description"`
	Visibility       string `json:"visibility"`
	AllowComments    *bool  `json:"allow_comments"`
	AllowVoting      *bool  `json:"allow_voting"`
	VotesPerCustomer *int   `json:"votes_per_customer"`
	Moderation       bool   `json:"moderation"`
	Position         int    `json:"position"`
}
type ItemInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Visibility  string `json:"visibility"`
	SubmitterID string `json:"submitter_id"`
	CompanyID   string `json:"company_id"`
	ProductArea string `json:"product_area"`
	Priority    string `json:"priority"`
}

func New(pool *database.Pool, eventLog *events.Log, auditLog *audit.Log) *Service {
	return &Service{pool: pool, events: eventLog, audit: auditLog}
}

func (s *Service) CreateBoard(ctx context.Context, workspaceID string, input BoardInput) (*Board, error) {
	name, slug := strings.TrimSpace(input.Name), strings.ToLower(strings.TrimSpace(input.Slug))
	if name == "" {
		return nil, ErrInvalidName
	}
	if slug == "" {
		return nil, ErrInvalidSlug
	}
	visibility := input.Visibility
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "private" && visibility != "invite_only" {
		return nil, errors.New("feedback: invalid visibility")
	}
	comments, voting := true, true
	if input.AllowComments != nil {
		comments = *input.AllowComments
	}
	if input.AllowVoting != nil {
		voting = *input.AllowVoting
	}
	id := ids.New(ids.PrefixFeedbackBoard)
	var board Board
	err := s.pool.QueryRow(ctx, `INSERT INTO feedback_boards(id,workspace_id,name,slug,description,visibility,allow_comments,allow_voting,votes_per_customer,moderation,position) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11) RETURNING id,workspace_id,name,slug,coalesce(description,''),visibility,allow_comments,allow_voting,votes_per_customer,CASE WHEN moderation THEN 'pre' ELSE 'none' END,item_count,position,created_at,updated_at`, id, workspaceID, name, slug, strings.TrimSpace(input.Description), visibility, comments, voting, input.VotesPerCustomer, input.Moderation, input.Position).Scan(&board.ID, &board.WorkspaceID, &board.Name, &board.Slug, &board.Description, &board.Visibility, &board.AllowComments, &board.AllowVoting, &board.VotesPerCustomer, &board.Moderation, &board.ItemCount, &board.Position, &board.CreatedAt, &board.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("feedback: create board: %w", err)
	}
	return &board, nil
}

func (s *Service) ListBoards(ctx context.Context, workspaceID string) ([]Board, error) {
	return s.ListBoardsPage(ctx, workspaceID, nil, "", 0)
}

// ListBoardsPage orders boards by their explicit dashboard position and uses
// the id as a deterministic tie-breaker. A position cursor is kept in the
// opaque API cursor's Value field because position is not a timestamp.
func (s *Service) ListBoardsPage(ctx context.Context, workspaceID string, beforePosition *int, beforeID string, limit int) ([]Board, error) {
	query := `SELECT id,workspace_id,name,slug,coalesce(description,''),visibility,allow_comments,allow_voting,votes_per_customer,CASE WHEN moderation THEN 'pre' ELSE 'none' END,item_count,position,created_at,updated_at FROM feedback_boards WHERE workspace_id=$1`
	args := []any{workspaceID}
	if beforePosition != nil {
		query += " AND (position,id) > ($2,$3)"
		args = append(args, *beforePosition, beforeID)
	}
	query += " ORDER BY position,id"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Board, 0)
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.WorkspaceID, &b.Name, &b.Slug, &b.Description, &b.Visibility, &b.AllowComments, &b.AllowVoting, &b.VotesPerCustomer, &b.Moderation, &b.ItemCount, &b.Position, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (s *Service) GetBoard(ctx context.Context, workspaceID, idOrSlug string, publicOnly bool) (*Board, error) {
	query := `SELECT id,workspace_id,name,slug,coalesce(description,''),visibility,allow_comments,allow_voting,votes_per_customer,CASE WHEN moderation THEN 'pre' ELSE 'none' END,item_count,position,created_at,updated_at FROM feedback_boards WHERE workspace_id=$1 AND (id=$2 OR slug=$2)`
	if publicOnly {
		query += ` AND visibility='public'`
	}
	var b Board
	err := s.pool.QueryRow(ctx, query, workspaceID, idOrSlug).Scan(&b.ID, &b.WorkspaceID, &b.Name, &b.Slug, &b.Description, &b.Visibility, &b.AllowComments, &b.AllowVoting, &b.VotesPerCustomer, &b.Moderation, &b.ItemCount, &b.Position, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Service) CreateItem(ctx context.Context, workspaceID, boardID, memberID string, input ItemInput, customerID string) (*Item, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, ErrInvalidName
	}
	typ := input.Type
	if typ == "" {
		typ = "feature_request"
	}
	if !itemTypes[typ] {
		return nil, ErrInvalidType
	}
	board, err := s.GetBoard(ctx, workspaceID, boardID, false)
	if err != nil {
		return nil, err
	}
	visibility := input.Visibility
	if visibility == "" {
		visibility = "public"
	}
	status := "open"
	if board.Moderation == "pre" {
		status = "held"
	}
	id := ids.New(ids.PrefixFeedbackItem)
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO feedback_items(id,workspace_id,board_id,title,description,type,status,visibility,submitter_id,created_by_member_id,company_id,product_area,priority) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),NULLIF($13,''))`, id, workspaceID, boardID, title, strings.TrimSpace(input.Description), typ, status, visibility, customerID, memberID, input.CompanyID, input.ProductArea, input.Priority); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE feedback_boards SET item_count=item_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, boardID); err != nil {
			return err
		}
		if s.events != nil {
			_, err := s.events.Append(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: events.FeedbackCreated, EntityType: "feedback_item", EntityID: id, ActorType: events.ActorUser, ActorID: memberID, Data: map[string]any{"board_id": boardID, "title": title}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("feedback: create item: %w", err)
	}
	return s.GetItem(ctx, workspaceID, id, customerID)
}

func (s *Service) ListItems(ctx context.Context, workspaceID, boardID, status, sort, query, customerID string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	order := `vote_count DESC,created_at DESC`
	if sort == "recent" {
		order = `created_at DESC`
	}
	rows, err := s.pool.Query(ctx, `SELECT i.id,i.workspace_id,i.board_id,i.title,i.description,i.type,i.status,i.visibility,i.submitter_id,i.company_id,i.product_area,i.priority,i.vote_count,i.comment_count,i.subscriber_count,i.merged_into_id,EXISTS(SELECT 1 FROM feedback_votes v WHERE v.item_id=i.id AND v.customer_id=$5),EXISTS(SELECT 1 FROM feedback_subscriptions fs WHERE fs.item_id=i.id AND fs.customer_id=$5),i.created_at,i.updated_at FROM feedback_items i WHERE i.workspace_id=$1 AND i.board_id=$2 AND i.merged_into_id IS NULL AND ($3='' OR i.status=$3) AND ($4='' OR i.title ILIKE '%'||$4||'%' OR i.description ILIKE '%'||$4||'%') ORDER BY `+order+` LIMIT $6`, workspaceID, boardID, status, strings.TrimSpace(query), customerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

// ListItemsPage supports both board sort modes without losing deterministic
// ordering. Vote sorting uses the cursor's vote count, creation timestamp,
// and id; recent sorting uses the timestamp and id pair.
func (s *Service) ListItemsPage(ctx context.Context, workspaceID, boardID, status, sort, query, customerID string, before time.Time, beforeID string, beforeVote *int64, limit int) ([]Item, error) {
	if limit <= 0 || limit > 201 {
		limit = 101
	}
	where := []string{"i.workspace_id=$1", "i.board_id=$2", "i.merged_into_id IS NULL", "($3='' OR i.status=$3)", "($4='' OR i.title ILIKE '%'||$4||'%' OR i.description ILIKE '%'||$4||'%')"}
	args := []any{workspaceID, boardID, status, strings.TrimSpace(query), customerID}
	order := "i.vote_count DESC,i.created_at DESC,i.id DESC"
	if sort == "recent" {
		order = "i.created_at DESC,i.id DESC"
		if !before.IsZero() {
			where = append(where, fmt.Sprintf("(i.created_at,i.id) < ($%d,$%d)", len(args)+1, len(args)+2))
			args = append(args, before, beforeID)
		}
	} else if beforeVote != nil && !before.IsZero() {
		where = append(where, fmt.Sprintf("(i.vote_count < $%d OR (i.vote_count = $%d AND (i.created_at,i.id) < ($%d,$%d)))", len(args)+1, len(args)+1, len(args)+2, len(args)+3))
		args = append(args, *beforeVote, before, beforeID)
	}
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, `SELECT i.id,i.workspace_id,i.board_id,i.title,i.description,i.type,i.status,i.visibility,i.submitter_id,i.company_id,i.product_area,i.priority,i.vote_count,i.comment_count,i.subscriber_count,i.merged_into_id,EXISTS(SELECT 1 FROM feedback_votes v WHERE v.item_id=i.id AND v.customer_id=$5),EXISTS(SELECT 1 FROM feedback_subscriptions fs WHERE fs.item_id=i.id AND fs.customer_id=$5),i.created_at,i.updated_at FROM feedback_items i WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+` LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (s *Service) ListRoadmapItems(ctx context.Context, workspaceID, status string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `SELECT i.id,i.workspace_id,i.board_id,i.title,i.description,i.type,i.status,i.visibility,i.submitter_id,i.company_id,i.product_area,i.priority,i.vote_count,i.comment_count,i.subscriber_count,i.merged_into_id,false,false,i.created_at,i.updated_at FROM feedback_items i JOIN feedback_boards b ON b.id=i.board_id AND b.workspace_id=i.workspace_id WHERE i.workspace_id=$1 AND b.visibility='public' AND i.merged_into_id IS NULL AND ($2='' OR i.status=$2) ORDER BY CASE i.status WHEN 'in_progress' THEN 1 WHEN 'planned' THEN 2 WHEN 'completed' THEN 3 ELSE 4 END,i.vote_count DESC,i.created_at DESC LIMIT $3`, workspaceID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (s *Service) GetItem(ctx context.Context, workspaceID, id, customerID string) (*Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.id,i.workspace_id,i.board_id,i.title,i.description,i.type,i.status,i.visibility,i.submitter_id,i.company_id,i.product_area,i.priority,i.vote_count,i.comment_count,i.subscriber_count,i.merged_into_id,EXISTS(SELECT 1 FROM feedback_votes v WHERE v.item_id=i.id AND v.customer_id=$3),EXISTS(SELECT 1 FROM feedback_subscriptions fs WHERE fs.item_id=i.id AND fs.customer_id=$3),i.created_at,i.updated_at FROM feedback_items i WHERE i.workspace_id=$1 AND i.id=$2`, workspaceID, id, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNotFound
	}
	return &items[0], nil
}

// Subscribe follows a public feedback item for the authenticated customer.
// The customer/workspace join is deliberately checked while the item row is
// locked so a customer from another workspace cannot create a cross-tenant
// subscription or affect the denormalized counter.
func (s *Service) Subscribe(ctx context.Context, workspaceID, itemID, customerID string) error {
	if strings.TrimSpace(customerID) == "" {
		return ErrCustomerRequired
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var lockedID string
		if err := tx.QueryRow(ctx, `SELECT i.id FROM feedback_items i JOIN customers c ON c.id=$3 AND c.workspace_id=i.workspace_id WHERE i.workspace_id=$1 AND i.id=$2 FOR UPDATE`, workspaceID, itemID, customerID).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `INSERT INTO feedback_subscriptions(item_id,customer_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, itemID, customerID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 1 {
			_, err = tx.Exec(ctx, `UPDATE feedback_items SET subscriber_count=subscriber_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, itemID)
		}
		return err
	})
}

// Unsubscribe stops feedback status notifications for the authenticated
// customer. It is idempotent so retries cannot make the counter negative.
func (s *Service) Unsubscribe(ctx context.Context, workspaceID, itemID, customerID string) error {
	if strings.TrimSpace(customerID) == "" {
		return ErrCustomerRequired
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var lockedID string
		if err := tx.QueryRow(ctx, `SELECT i.id FROM feedback_items i JOIN customers c ON c.id=$3 AND c.workspace_id=i.workspace_id WHERE i.workspace_id=$1 AND i.id=$2 FOR UPDATE`, workspaceID, itemID, customerID).Scan(&lockedID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		result, err := tx.Exec(ctx, `DELETE FROM feedback_subscriptions WHERE item_id=$1 AND customer_id=$2`, itemID, customerID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 1 {
			_, err = tx.Exec(ctx, `UPDATE feedback_items SET subscriber_count=GREATEST(0,subscriber_count-1),updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, itemID)
		}
		return err
	})
}

func (s *Service) SetStatus(ctx context.Context, workspaceID, itemID, memberID, status, note string) (*Item, error) {
	if !statuses[status] {
		return nil, ErrInvalidStatus
	}
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var from string
		if err := tx.QueryRow(ctx, `SELECT status FROM feedback_items WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, itemID).Scan(&from); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE feedback_items SET status=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, itemID, status); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO feedback_status_history(id,item_id,from_status,to_status,note,actor_id) VALUES($1,$2,NULLIF($3,''),$4,NULLIF($5,''),NULLIF($6,''))`, ids.New(ids.PrefixStatusHistory), itemID, from, status, note, memberID); err != nil {
			return err
		}
		if s.events != nil && from != status {
			if _, err := s.events.Append(ctx, tx, events.Event{WorkspaceID: workspaceID, Type: events.FeedbackStatusChanged, EntityType: "feedback_item", EntityID: itemID, ActorType: events.ActorUser, ActorID: memberID, Data: map[string]any{"from": from, "to": status}}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetItem(ctx, workspaceID, itemID, "")
}

func (s *Service) Vote(ctx context.Context, workspaceID, itemID, customerID string) error {
	if customerID == "" {
		return ErrAlreadyVoted
	}
	return database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var boardID string
		var enabled bool
		var maxVotes *int
		if err := tx.QueryRow(ctx, `SELECT b.id,b.allow_voting,b.votes_per_customer FROM feedback_items i JOIN feedback_boards b ON b.id=i.board_id WHERE i.workspace_id=$1 AND i.id=$2 FOR UPDATE`, workspaceID, itemID).Scan(&boardID, &enabled, &maxVotes); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if !enabled {
			return ErrVotingDisabled
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM feedback_votes WHERE item_id=$1 AND customer_id=$2)`, itemID, customerID).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrAlreadyVoted
		}
		if maxVotes != nil {
			var used int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM feedback_votes v JOIN feedback_items i ON i.id=v.item_id WHERE v.workspace_id=$1 AND v.customer_id=$2 AND i.board_id=$3`, workspaceID, customerID, boardID).Scan(&used); err != nil {
				return err
			}
			if used >= *maxVotes {
				return ErrVoteLimit
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO feedback_votes(item_id,customer_id,workspace_id) VALUES($1,$2,$3)`, itemID, customerID, workspaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE feedback_items SET vote_count=vote_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, itemID)
		return err
	})
}

func (s *Service) AddComment(ctx context.Context, workspaceID, itemID, authorType, authorID, authorName, body string, official bool) (*Comment, error) {
	if strings.TrimSpace(body) == "" {
		return nil, ErrInvalidComment
	}
	item, err := s.GetItem(ctx, workspaceID, itemID, "")
	if err != nil {
		return nil, err
	}
	board, err := s.GetBoard(ctx, workspaceID, item.BoardID, false)
	if err != nil {
		return nil, err
	}
	if !board.AllowComments {
		return nil, ErrCommentsDisabled
	}
	id := ids.New(ids.PrefixFeedbackComment)
	var c Comment
	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO feedback_comments(id,workspace_id,item_id,author_type,author_id,author_name,body,is_official_update) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8)`, id, workspaceID, itemID, authorType, authorID, authorName, strings.TrimSpace(body), official); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE feedback_items SET comment_count=comment_count+1,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, itemID)
		return err
	})
	if err != nil {
		return nil, err
	}
	err = s.pool.QueryRow(ctx, `SELECT id,workspace_id,item_id,author_type,author_id,author_name,body,is_official_update,created_at FROM feedback_comments WHERE id=$1`, id).Scan(&c.ID, &c.WorkspaceID, &c.ItemID, &c.AuthorType, &c.AuthorID, &c.AuthorName, &c.Body, &c.OfficialUpdate, &c.CreatedAt)
	return &c, err
}

func (s *Service) ListComments(ctx context.Context, workspaceID, itemID string, limit int) ([]Comment, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT c.id,c.workspace_id,c.item_id,c.author_type,c.author_id,c.author_name,c.body,c.is_official_update,c.created_at FROM feedback_comments c JOIN feedback_items i ON i.id=c.item_id AND i.workspace_id=c.workspace_id WHERE c.workspace_id=$1 AND c.item_id=$2 ORDER BY c.created_at ASC,c.id ASC LIMIT $3`, workspaceID, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := make([]Comment, 0)
	for rows.Next() {
		var comment Comment
		if err := rows.Scan(&comment.ID, &comment.WorkspaceID, &comment.ItemID, &comment.AuthorType, &comment.AuthorID, &comment.AuthorName, &comment.Body, &comment.OfficialUpdate, &comment.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

func scanItems(rows pgx.Rows) ([]Item, error) {
	result := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.BoardID, &item.Title, &item.Description, &item.Type, &item.Status, &item.Visibility, &item.SubmitterID, &item.CompanyID, &item.ProductArea, &item.Priority, &item.VoteCount, &item.CommentCount, &item.SubscriberCount, &item.MergedIntoID, &item.ViewerHasVoted, &item.ViewerSubscribed, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
