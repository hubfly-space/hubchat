// Package database owns the PostgreSQL connection pool, migration
// application, and the transaction helper every module builds on.
//
// # Responsibilities
//
// Pool configuration, statement timeouts, advisory locks, migration
// application, and the transaction wrapper every module uses.
//
// # Boundary
//
// The migration runner takes an advisory lock so several instances starting
// at once apply migrations exactly once (§18). No module reaches around this
// package to open its own connection.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hubchat/hubchat/internal/config"
)

// Pool wraps *pgxpool.Pool. Kept as a named type so call sites depend on this
// package's name rather than pgx's, which is what lets every other module
// import "database" instead of "pgx" directly.
type Pool = pgxpool.Pool

// Open configures and connects a pool per cfg.Database, applying the
// statement timeout to every connection so a single runaway query cannot
// starve the pool (§17).
func Open(ctx context.Context, cfg config.Database) (*Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("database: parse connection string: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.ConnMaxLifetime

	statementTimeoutMS := cfg.StatementTimeout.Milliseconds()
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", statementTimeoutMS)

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}

	return pool, nil
}
