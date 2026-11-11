package store

import (
	"context"
	"encoding/json"
)

// AccountRec is one registered account persisted in the kv table. Order is
// significant: the first record is the primary account.
type AccountRec struct {
	Acct  string `json:"acct"`
	Proto string `json:"proto"`
}

const kvAccounts = "accounts"

// Accounts returns the registered account list in primary-first order.
func (d *DB) Accounts(ctx context.Context) ([]AccountRec, error) {
	v, err := d.GetKV(ctx, kvAccounts)
	if err != nil || v == "" {
		return nil, err
	}
	var recs []AccountRec
	if err := json.Unmarshal([]byte(v), &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// SetAccounts persists the registered account list.
func (d *DB) SetAccounts(ctx context.Context, recs []AccountRec) error {
	b, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	return d.SetKV(ctx, kvAccounts, string(b))
}
