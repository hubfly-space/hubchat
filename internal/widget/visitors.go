package widget

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/hubchat/hubchat/internal/auth"
	"github.com/hubchat/hubchat/internal/customer"
	"github.com/hubchat/hubchat/internal/database"
	"github.com/hubchat/hubchat/internal/ids"
)

// IssueVisitor mints a new anonymous browser identity, returning the raw
// token — stored hashed, exactly like a session (§11.5), and handed to the
// browser once to persist in its own storage. Nothing ties this to a widget
// specifically: a visitor belongs to the workspace, so it survives a switch
// between widget instances (production vs. test) or between chat and a
// future portal handoff.
func (s *Service) IssueVisitor(ctx context.Context, workspaceID string) (token string, visitor *Visitor, err error) {
	token, err = auth.NewToken()
	if err != nil {
		return "", nil, err
	}
	id := ids.New(ids.PrefixVisitor)
	visitor, err = s.repo.insertVisitor(ctx, id, workspaceID, auth.HashToken(token))
	if err != nil {
		return "", nil, err
	}
	return token, visitor, nil
}

// ResolveVisitor authenticates a visitor token, returning the visitor it
// names. Every widget entry point that isn't the initial config fetch calls
// this first — a widget conversation, a reply, an event, an identify call.
func (s *Service) ResolveVisitor(ctx context.Context, workspaceID, token string) (*Visitor, error) {
	if token == "" {
		return nil, ErrVisitorInvalid
	}
	return s.repo.visitorByTokenHash(ctx, workspaceID, auth.HashToken(token))
}

// IdentifyInput is what the widget SDK's identify() call carries, whether or
// not it includes a signed token.
type IdentifyInput struct {
	Name       *string
	Email      *string
	ExternalID *string
	// SignedToken, when present, is verified against this workspace's derived
	// key (see identity_token.go) and — only then — treated as proof rather
	// than a bare claim (§6.9: never merge on weak signals).
	SignedToken *string
}

// Identify links a visitor to a customer record, creating one if this is the
// first time this person has been seen from this visitor.
func (s *Service) Identify(ctx context.Context, workspaceID string, visitor *Visitor, in IdentifyInput) (*customer.Customer, error) {
	name, email, externalID := in.Name, in.Email, in.ExternalID
	verified := false
	method := "manual"

	if in.SignedToken != nil && *in.SignedToken != "" {
		claims, err := verifyIdentityToken(s.secretKey, workspaceID, *in.SignedToken)
		if err != nil {
			return nil, err
		}
		verified = true
		method = "identity_token"
		subject := claims.Subject
		externalID = &subject
		if claims.Email != "" {
			email = &claims.Email
		}
		if claims.Name != "" {
			name = &claims.Name
		}
	}

	var existingCustomerID *string
	if visitor.CustomerID != nil {
		existingCustomerID = visitor.CustomerID
	}

	found, err := s.customer.Identify(ctx, workspaceID, existingCustomerID, name, email, externalID, verified)
	if err != nil {
		return nil, err
	}

	// Already linked to exactly this customer — nothing new to record.
	if visitor.CustomerID != nil && *visitor.CustomerID == found.ID {
		return found, nil
	}

	err = database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.repo.linkVisitor(ctx, tx, ids.New(ids.PrefixVisitorLink), visitor.ID, found.ID, method)
	})
	if err != nil {
		return nil, err
	}
	visitor.CustomerID = &found.ID
	return found, nil
}
