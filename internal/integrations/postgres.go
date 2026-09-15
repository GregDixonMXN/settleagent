package integrations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/settleagent/settleagent/internal/actions"
	"github.com/settleagent/settleagent/internal/domain"
)

// Postgres read-only queries. v0.2 executes SELECT only: single statement,
// read-only transaction, statement timeout, row cap. Writes, DDL, and
// multi-statements are rejected with an explanation — not silently coerced.
func postgresQueryHandler(getCred func(orgID string) (string, bool)) actions.ToolHandler {
	return func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		dsn, ok := getCred(a.OrgID)
		if !ok {
			return nil, fmt.Errorf("postgres integration not configured for this org")
		}
		sql, ok := strArg(a.Arguments, "sql", "query")
		if !ok {
			return nil, fmt.Errorf("postgres.query needs a sql string")
		}
		body := strings.TrimSpace(sql)
		upper := strings.ToUpper(body)
		if !(strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") || strings.HasPrefix(upper, "VALUES") || strings.HasPrefix(upper, "TABLE") || strings.HasPrefix(upper, "EXPLAIN")) {
			return nil, fmt.Errorf("postgres.query executes read-only statements (SELECT/WITH/VALUES/TABLE/EXPLAIN); got %.20s", body)
		}
		trimmed := strings.TrimRight(body, "; \t\n")
		if strings.Contains(trimmed, ";") {
			return nil, fmt.Errorf("postgres.query rejects multi-statement input")
		}
		connCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		conn, err := pgx.Connect(connCtx, dsn)
		if err != nil {
			return nil, actions.Uncertain(fmt.Errorf("postgres unreachable (may be transient): %w", err))
		}
		defer conn.Close(context.Background())
		tx, err := conn.BeginTx(connCtx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, fmt.Errorf("postgres begin failed: %w", err)
		}
		defer tx.Rollback(connCtx)
		if _, err := tx.Exec(connCtx, `SET LOCAL statement_timeout = '10s'`); err != nil {
			return nil, fmt.Errorf("postgres guard failed: %w", err)
		}
		rows, err := tx.Query(connCtx, trimmed)
		if err != nil {
			return nil, fmt.Errorf("postgres query failed: %w", err)
		}
		defer rows.Close()
		cols := []string{}
		for _, f := range rows.FieldDescriptions() {
			cols = append(cols, f.Name)
		}
		out := []any{}
		for rows.Next() && len(out) < 100 {
			vals, err := rows.Values()
			if err != nil {
				return nil, fmt.Errorf("postgres row decode failed: %w", err)
			}
			row := map[string]any{}
			for i, c := range cols {
				row[c] = vals[i]
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("postgres rows failed: %w", err)
		}
		if err := tx.Commit(connCtx); err != nil {
			return nil, fmt.Errorf("postgres commit failed: %w", err)
		}
		return map[string]any{"columns": cols, "rows": out, "truncated": len(out) == 100}, nil
	}
}

func postgresClasses() map[string][]domain.ActionClass {
	return map[string][]domain.ActionClass{
		"query": {domain.ClassReadOnly},
	}
}
