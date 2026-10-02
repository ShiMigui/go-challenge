package dto

import (
	"time"

	"github.com/shimigui/go-challenge/internal/domain/wager"
)

// Wagering types re-exported from domain for API layer.
type TransactionKind = wager.Kind
type TransactionState = wager.State

const (
	KindOpening  = wager.KindOpening
	KindBet      = wager.KindBet
	KindWin      = wager.KindWin
	KindLoss     = wager.KindLoss
	KindRefund   = wager.KindRefund
	KindRollback = wager.KindRollback

	StatePending          = wager.StatePending
	StatePendingReference = wager.StatePendingReference
	StateProcessed        = wager.StateProcessed
	StateRejected         = wager.StateRejected
	StateFailed           = wager.StateFailed
)

type CreateTransactionRequest struct {
	ProviderID                     string          `json:"providerId"`
	ExternalTransactionID          string          `json:"externalTransactionId"`
	PlayerID                       string          `json:"playerId"`
	WalletID                       string          `json:"walletId"`
	RoundID                        string          `json:"roundId,omitempty"`
	GameID                         string          `json:"gameId,omitempty"`
	Kind                           TransactionKind `json:"kind"`
	Money                          MoneyPayload    `json:"money"`
	ReferenceExternalTransactionID string          `json:"referenceExternalTransactionId,omitempty"`
}

type CreateTransactionResponse struct {
	TransactionID        string           `json:"transactionId"`
	Status               TransactionState `json:"status"`
	Balance              *MoneyPayload    `json:"balance,omitempty"`
	FailureCode          string           `json:"failureCode,omitempty"`
	ReferenceNextAttempt *time.Time       `json:"referenceNextAttempt,omitempty"`
	IdempotentReplay     bool             `json:"idempotentReplay"`
}

type TransactionResponse struct {
	TransactionID                  string           `json:"transactionId"`
	Kind                           TransactionKind  `json:"kind"`
	State                          TransactionState `json:"state"`
	PlayerID                       string           `json:"playerId"`
	WalletID                       string           `json:"walletId"`
	RoundID                        string           `json:"roundId,omitempty"`
	GameID                         string           `json:"gameId,omitempty"`
	ProviderID                     string           `json:"providerId,omitempty"`
	ExternalTransactionID          string           `json:"externalTransactionId,omitempty"`
	Money                          MoneyPayload     `json:"money"`
	ObservedBalance                *MoneyPayload    `json:"observedBalance,omitempty"`
	ReferenceExternalTransactionID string           `json:"referenceExternalTransactionId,omitempty"`
	ReferenceTransactionID         string           `json:"referenceTransactionId,omitempty"`
	FailureCode                    string           `json:"failureCode,omitempty"`
	FailureMessage                 string           `json:"failureMessage,omitempty"`
	ReferenceAttempts              int              `json:"referenceAttempts"`
	ReferenceNextAttempt           *time.Time       `json:"referenceNextAttempt,omitempty"`
	CreatedAt                      time.Time        `json:"createdAt"`
	UpdatedAt                      time.Time        `json:"updatedAt"`
	ProcessedAt                    *time.Time       `json:"processedAt,omitempty"`
}

type ReconciliationRequest struct {
	WalletID string `json:"walletId" param:"walletId"`
}

type ReconciliationResponse struct {
	WalletID          string       `json:"walletId"`
	StoredBalance     MoneyPayload `json:"storedBalance"`
	CalculatedBalance MoneyPayload `json:"calculatedBalance"`
	Difference        MoneyPayload `json:"difference"`
	Consistent        bool         `json:"consistent"`
	CheckedEntries    int          `json:"checkedEntries"`
}
