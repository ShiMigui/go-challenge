package wallet

import (
	"context"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
)

// NowFunc allows injecting time for testing.
type NowFunc func() time.Time

// Service defines wallet operations.
type Service interface {
	CreateWallet(ctx context.Context, playerID string, opening money.Money, now NowFunc) (*Wallet, error)
	GetWallet(ctx context.Context, id string) (*Wallet, error)
	GetLedger(ctx context.Context, walletID string, cursor *string, limit int) ([]*ledger.Entry, *string, error)
	Reconcile(ctx context.Context, walletID string) (*Reconciliation, error)
}

// Reconciliation holds the result of a wallet reconciliation.
type Reconciliation struct {
	WalletID          string
	StoredBalance     money.Money
	CalculatedBalance money.Money
	Difference        money.Money
	Consistent        bool
	CheckedEntries    int
}
