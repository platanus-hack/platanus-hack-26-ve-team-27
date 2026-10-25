package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/platanus-hack/platanus-hack-26-ve-team-27/server/internal/contract"
)

// ErrCallNotFound means no call has the given conversation_id.
var ErrCallNotFound = errors.New("db: call not found")

// SaveVerdict stores the duress detector's verdict and reason on the call
// with conversationID, stamping verdict_at with the database clock. A
// repeated webhook overwrites the earlier verdict. The calls.verdict CHECK
// constraint rejects a value that is not a contract.Verdict. It clears
// calls.ficha; SaveVerdictWithContext stores one.
func SaveVerdict(ctx context.Context, pool *sql.DB, conversationID string, verdict contract.Verdict, reason string) error {
	return SaveVerdictWithContext(ctx, pool, conversationID, verdict, reason, nil)
}

// SaveVerdictWithContext is SaveVerdict that also stores the call's context
// (#106) in calls.ficha; nil stores NULL.
func SaveVerdictWithContext(ctx context.Context, pool *sql.DB, conversationID string, verdict contract.Verdict, reason string, ficha *contract.CallContext) error {
	var raw []byte
	if ficha != nil {
		var err error
		if raw, err = json.Marshal(ficha); err != nil {
			return err
		}
	}
	res, err := pool.ExecContext(ctx,
		`UPDATE calls SET verdict = $2, verdict_reason = $3, verdict_at = now(), ficha = $4::jsonb WHERE conversation_id = $1`,
		conversationID, string(verdict), reason, nullJSON(raw))
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

// nullJSON is raw as a query argument, NULL when empty.
func nullJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
