package wager

import (
	"context"
)

// Service define as operações de wagering.
type Service interface {
	SubmitTransaction(ctx context.Context, params ExternalParams) (*Transaction, error)
	GetTransaction(ctx context.Context, id string) (*Transaction, error)
	GetTransactionByExternal(ctx context.Context, providerID, externalID string) (*Transaction, error)
}
