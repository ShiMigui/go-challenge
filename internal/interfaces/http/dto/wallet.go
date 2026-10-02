package dto

import "time"

// ===== Wallet =====

type CreateWalletRequest struct {
	PlayerID       string       `json:"playerId"`
	InitialBalance MoneyPayload `json:"initialBalance"`
}

type CreateWalletResponse struct {
	ID       string       `json:"id"`
	PlayerID string       `json:"playerId"`
	Balance  MoneyPayload `json:"balance"`
	Version  int64        `json:"version"`
}

type WalletResponse struct {
	ID       string       `json:"id"`
	PlayerID string       `json:"playerId"`
	Balance  MoneyPayload `json:"balance"`
	Version  int64        `json:"version"`
}

type LedgerEntryResponse struct {
	ID            string       `json:"id"`
	TransactionID string       `json:"transactionId"`
	Direction     string       `json:"direction"` // DEBIT | CREDIT
	Amount        MoneyPayload `json:"amount"`
	BalanceBefore MoneyPayload `json:"balanceBefore"`
	BalanceAfter  MoneyPayload `json:"balanceAfter"`
	CreatedAt     time.Time    `json:"createdAt"`
}

type LedgerPageResponse struct {
	Entries    []LedgerEntryResponse `json:"entries"`
	NextCursor *string               `json:"nextCursor,omitempty"`
	Limit      int                   `json:"limit"`
}
