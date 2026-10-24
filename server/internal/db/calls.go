package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// ErrCallNotFound means no call has the given conversation_id.
var ErrCallNotFound = errors.New("db: call not found")

// SaveVerdict stores the duress detector's verdict and reason on the call
// with conversationID, stamping verdict_at with the database clock. A
// repeated webhook overwrites the earlier verdict. The calls.verdict CHECK
// constraint rejects a value that is not a contract.Verdict.
func SaveVerdict(ctx context.Context, pool *sql.DB, conversationID string, verdict contract.Verdict, reason string) error {
	res, err := pool.ExecContext(ctx,
		`UPDATE calls SET verdict = $2, verdict_reason = $3, verdict_at = now() WHERE conversation_id = $1`,
		conversationID, string(verdict), reason)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrCallNotFound
	}
	return nil
}
