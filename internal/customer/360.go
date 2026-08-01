package customer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Customer360 is the bounded cross-module snapshot shown on the customer
// detail page. It is intentionally a snapshot rather than a second copy of
// the source records: each section keeps the owning record's id so a future
// deep link can open the authoritative screen.
type Customer360 struct {
	Feedback            []FeedbackReference `json:"feedback"`
	FeedbackTruncated   bool                `json:"feedback_truncated"`
	Surveys             []SurveyReference   `json:"surveys"`
	SurveysTruncated    bool                `json:"surveys_truncated"`
	Articles            []ArticleReference  `json:"articles"`
	ArticlesTruncated   bool                `json:"articles_truncated"`
	Identities          []IdentityReference `json:"identities"`
	IdentitiesTruncated bool                `json:"identities_truncated"`
	Merges              []MergeReference    `json:"merges"`
	MergesTruncated     bool                `json:"merges_truncated"`
}

type FeedbackReference struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	BoardID      string    `json:"board_id"`
	BoardName    string    `json:"board_name"`
	Status       string    `json:"status"`
	VoteCount    int       `json:"vote_count"`
	CommentCount int       `json:"comment_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type SurveyReference struct {
	ID          string     `json:"id"`
	SurveyID    string     `json:"survey_id"`
	SurveyName  string     `json:"survey_name"`
	SurveyType  string     `json:"survey_type"`
	Score       *float64   `json:"score,omitempty"`
	Comment     string     `json:"comment,omitempty"`
	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
}

type ArticleReference struct {
	ID        string    `json:"id"`
	ArticleID string    `json:"article_id"`
	Title     string    `json:"title"`
	Slug      string    `json:"slug"`
	Helpful   bool      `json:"helpful"`
	Comment   string    `json:"comment,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type IdentityReference struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Value      string     `json:"value"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	IsPrimary  bool       `json:"is_primary"`
	CreatedAt  time.Time  `json:"created_at"`
}

type MergeReference struct {
	ID              string         `json:"id"`
	WinnerID        string         `json:"winner_id"`
	LoserID         string         `json:"loser_id"`
	MovedCounts     map[string]any `json:"moved_counts"`
	ReversibleUntil *time.Time     `json:"reversible_until,omitempty"`
	ReversedAt      *time.Time     `json:"reversed_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
}

const (
	customer360PageSize  = 25
	customer360QuerySize = customer360PageSize + 1
)

// Customer360 loads the recent cross-module records associated with a
// customer. Every query carries both workspace_id and customer_id. The
// extra row in each result is retained only to tell the caller that the
// bounded snapshot was truncated; the UI can then link to the owning list.
func (s *Service) Customer360(ctx context.Context, workspaceID, customerID string) (*Customer360, error) {
	if _, err := s.repo.byID(ctx, workspaceID, customerID); err != nil {
		return nil, err
	}

	result := &Customer360{}

	feedbackRows, err := s.pool.Query(ctx, `
		SELECT fi.id, fi.title, fi.board_id, fb.name, fi.status,
		       fi.vote_count, fi.comment_count, fi.created_at
		FROM feedback_items fi
		JOIN feedback_boards fb ON fb.id=fi.board_id AND fb.workspace_id=$1
		WHERE fi.workspace_id=$1 AND fi.merged_into_id IS NULL
		  AND (fi.submitter_id=$2
		       OR EXISTS (SELECT 1 FROM feedback_votes fv
		                 WHERE fv.workspace_id=$1 AND fv.item_id=fi.id AND fv.customer_id=$2)
		       OR EXISTS (SELECT 1 FROM feedback_subscriptions fs
		                 JOIN feedback_items fsi ON fsi.id=fs.item_id AND fsi.workspace_id=$1
		                 WHERE fs.item_id=fi.id AND fs.customer_id=$2))
		ORDER BY fi.created_at DESC, fi.id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 feedback: %w", err)
	}
	for feedbackRows.Next() {
		var item FeedbackReference
		if err := feedbackRows.Scan(&item.ID, &item.Title, &item.BoardID, &item.BoardName, &item.Status, &item.VoteCount, &item.CommentCount, &item.CreatedAt); err != nil {
			feedbackRows.Close()
			return nil, fmt.Errorf("customer: scan 360 feedback: %w", err)
		}
		result.Feedback = append(result.Feedback, item)
	}
	if err := feedbackRows.Err(); err != nil {
		feedbackRows.Close()
		return nil, fmt.Errorf("customer: read 360 feedback: %w", err)
	}
	feedbackRows.Close()
	if len(result.Feedback) > customer360PageSize {
		result.Feedback = result.Feedback[:customer360PageSize]
		result.FeedbackTruncated = true
	}

	surveyRows, err := s.pool.Query(ctx, `
		SELECT sr.id, sr.survey_id, s.name, s.type, sr.score, sr.comment, sr.submitted_at
		FROM survey_responses sr
		JOIN surveys s ON s.id=sr.survey_id AND s.workspace_id=$1
		WHERE sr.workspace_id=$1 AND sr.customer_id=$2 AND sr.submitted_at IS NOT NULL
		ORDER BY sr.submitted_at DESC, sr.id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 surveys: %w", err)
	}
	for surveyRows.Next() {
		var item SurveyReference
		if err := surveyRows.Scan(&item.ID, &item.SurveyID, &item.SurveyName, &item.SurveyType, &item.Score, &item.Comment, &item.SubmittedAt); err != nil {
			surveyRows.Close()
			return nil, fmt.Errorf("customer: scan 360 survey: %w", err)
		}
		result.Surveys = append(result.Surveys, item)
	}
	if err := surveyRows.Err(); err != nil {
		surveyRows.Close()
		return nil, fmt.Errorf("customer: read 360 surveys: %w", err)
	}
	surveyRows.Close()
	if len(result.Surveys) > customer360PageSize {
		result.Surveys = result.Surveys[:customer360PageSize]
		result.SurveysTruncated = true
	}

	articleRows, err := s.pool.Query(ctx, `
		SELECT af.id, af.article_id, a.title, a.slug::text, af.helpful, af.comment, af.created_at
		FROM article_feedback af
		JOIN articles a ON a.id=af.article_id AND a.workspace_id=$1
		WHERE af.workspace_id=$1 AND af.customer_id=$2
		ORDER BY af.created_at DESC, af.id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 articles: %w", err)
	}
	for articleRows.Next() {
		var item ArticleReference
		if err := articleRows.Scan(&item.ID, &item.ArticleID, &item.Title, &item.Slug, &item.Helpful, &item.Comment, &item.CreatedAt); err != nil {
			articleRows.Close()
			return nil, fmt.Errorf("customer: scan 360 article: %w", err)
		}
		result.Articles = append(result.Articles, item)
	}
	if err := articleRows.Err(); err != nil {
		articleRows.Close()
		return nil, fmt.Errorf("customer: read 360 articles: %w", err)
	}
	articleRows.Close()
	if len(result.Articles) > customer360PageSize {
		result.Articles = result.Articles[:customer360PageSize]
		result.ArticlesTruncated = true
	}

	identityRows, err := s.pool.Query(ctx, `
		SELECT kind, id, value, verified_at, is_primary, created_at
		FROM (
			SELECT 'email'::text AS kind, id, email::text AS value, verified_at, is_primary, created_at
			FROM customer_emails WHERE workspace_id=$1 AND customer_id=$2
			UNION ALL
			SELECT 'phone'::text AS kind, id, phone AS value, verified_at, is_primary, created_at
			FROM customer_phones WHERE workspace_id=$1 AND customer_id=$2
		) identities
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 identities: %w", err)
	}
	for identityRows.Next() {
		var item IdentityReference
		if err := identityRows.Scan(&item.Kind, &item.ID, &item.Value, &item.VerifiedAt, &item.IsPrimary, &item.CreatedAt); err != nil {
			identityRows.Close()
			return nil, fmt.Errorf("customer: scan 360 identity: %w", err)
		}
		result.Identities = append(result.Identities, item)
	}
	if err := identityRows.Err(); err != nil {
		identityRows.Close()
		return nil, fmt.Errorf("customer: read 360 identities: %w", err)
	}
	identityRows.Close()
	if len(result.Identities) > customer360PageSize {
		result.Identities = result.Identities[:customer360PageSize]
		result.IdentitiesTruncated = true
	}

	mergeRows, err := s.pool.Query(ctx, `
		SELECT id, winner_id, loser_id, moved_counts, reversible_until, reversed_at, created_at
		FROM identity_merge_history
		WHERE workspace_id=$1 AND (winner_id=$2 OR loser_id=$2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, workspaceID, customerID, customer360QuerySize)
	if err != nil {
		return nil, fmt.Errorf("customer: load 360 merges: %w", err)
	}
	for mergeRows.Next() {
		var item MergeReference
		var raw []byte
		if err := mergeRows.Scan(&item.ID, &item.WinnerID, &item.LoserID, &raw, &item.ReversibleUntil, &item.ReversedAt, &item.CreatedAt); err != nil {
			mergeRows.Close()
			return nil, fmt.Errorf("customer: scan 360 merge: %w", err)
		}
		item.MovedCounts = map[string]any{}
		if len(raw) > 0 && json.Unmarshal(raw, &item.MovedCounts) != nil {
			item.MovedCounts = map[string]any{}
		}
		result.Merges = append(result.Merges, item)
	}
	if err := mergeRows.Err(); err != nil {
		mergeRows.Close()
		return nil, fmt.Errorf("customer: read 360 merges: %w", err)
	}
	mergeRows.Close()
	if len(result.Merges) > customer360PageSize {
		result.Merges = result.Merges[:customer360PageSize]
		result.MergesTruncated = true
	}

	return result, nil
}
