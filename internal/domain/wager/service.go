package wager

import (
	"context"

	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

// Service defines wagering operations.
type Service interface {
	SubmitTransaction(ctx context.Context, params ExternalParams) (*Transaction, error)
	GetTransaction(ctx context.Context, id string) (*Transaction, error)
	GetTransactionByExternal(ctx context.Context, providerID, externalID string) (*Transaction, error)
	ReconcileWallet(ctx context.Context, walletID string) (*wallet.Reconciliation, error)
}
