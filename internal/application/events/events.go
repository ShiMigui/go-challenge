// Package events concentra os fatos de domínio publicados pela aplicação.
//
// Cada evento tem um tipo concreto, um builder de payload JSON determinístico
// e um reconstrutor usado pela camada HTTP para repetir o hash canônico do
// payload de entrada. O pacote não conhece a infraestrutura: só monta o
// conteúdo, quem persiste é a outbox.
package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
)

// Tipos de evento publicados pela aplicação.
//
// O catálogo é fechado: todo evento que sai daqui pertence a um destes
// quatro fatos, exigidos pelo desafio (§11).
const (
	// TypeWagerTransactionProcessed anuncia a conclusão de uma operação.
	TypeWagerTransactionProcessed = "WagerTransactionProcessed"
	// TypeWagerTransactionRejected anuncia uma recusa por regra de negócio.
	TypeWagerTransactionRejected = "WagerTransactionRejected"
	// TypeWalletBalanceChanged anuncia a mudança do saldo de uma carteira.
	TypeWalletBalanceChanged = "WalletBalanceChanged"
	// TypeWagerTransactionPendingReference anuncia a espera por uma referência.
	TypeWagerTransactionPendingReference = "WagerTransactionPendingReference"
)

// moneyValue é a representação canônica de um valor monetário nos payloads:
// moeda por extenso e valor com duas casas, como no contrato de entrada.
type moneyValue struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func toMoneyValue(m money.Money) moneyValue {
	return moneyValue{Amount: m.String(), Currency: string(m.Currency())}
}

// NewEvent monta o envelope da outbox com identidade nova.
//
// O agregado de origem é quem conta a história: transações contam no
// agregado wager_transaction e o saldo no agregado wallet.
func NewEvent(aggregateType, aggregateID, eventType string, payload []byte, now time.Time) (*event.Event, error) {
	return event.NewEvent(identifier.New(), aggregateType, aggregateID, eventType, 1, payload, now)
}

// ProcessedPayload é o conteúdo de WagerTransactionProcessed.
type ProcessedPayload struct {
	TransactionID         string      `json:"transactionId"`
	Kind                  string      `json:"kind"`
	Status                string      `json:"status"`
	PlayerID              string      `json:"playerId"`
	WalletID              string      `json:"walletId"`
	ProviderID            string      `json:"providerId,omitempty"`
	ExternalTransactionID string      `json:"externalTransactionId,omitempty"`
	RoundID               string      `json:"roundId,omitempty"`
	GameID                string      `json:"gameId,omitempty"`
	Money                 moneyValue  `json:"money"`
	ObservedBalance       *moneyValue `json:"observedBalance,omitempty"`
	ProcessedAt           time.Time   `json:"processedAt"`
}

// WagerTransactionProcessed monta o payload da operação concluída.
//
// Vale para OPENING, BET, WIN, LOSS e reversões: todo PROCESSED carrega o
// saldo que a carteira apresentava no instante da conclusão, que é o valor
// que o replay deve devolver.
func WagerTransactionProcessed(tx *wager.Transaction) ([]byte, error) {
	p := ProcessedPayload{
		TransactionID:         tx.ID(),
		Kind:                  string(tx.Kind()),
		Status:                string(wager.StateProcessed),
		PlayerID:              tx.PlayerID(),
		WalletID:              tx.WalletID(),
		ProviderID:            tx.ProviderID(),
		ExternalTransactionID: tx.ExternalID(),
		RoundID:               tx.RoundID(),
		GameID:                tx.GameID(),
		Money:                 toMoneyValue(tx.Amount()),
		ProcessedAt:           tx.ProcessedAt(),
	}
	if tx.HasObservedBalance() {
		obs := toMoneyValue(tx.ObservedBalance())
		p.ObservedBalance = &obs
	}
	return json.Marshal(p)
}

// RejectedPayload é o conteúdo de WagerTransactionRejected.
type RejectedPayload struct {
	TransactionID         string     `json:"transactionId"`
	Kind                  string     `json:"kind"`
	Status                string     `json:"status"`
	PlayerID              string     `json:"playerId"`
	WalletID              string     `json:"walletId"`
	ProviderID            string     `json:"providerId,omitempty"`
	ExternalTransactionID string     `json:"externalTransactionId,omitempty"`
	Money                 moneyValue `json:"money"`
	FailureCode           string     `json:"failureCode"`
	FailureMessage        string     `json:"failureMessage,omitempty"`
	RejectedAt            time.Time  `json:"rejectedAt"`
}

// WagerTransactionRejected monta o payload da recusa por regra de negócio.
func WagerTransactionRejected(tx *wager.Transaction) ([]byte, error) {
	return json.Marshal(RejectedPayload{
		TransactionID:         tx.ID(),
		Kind:                  string(tx.Kind()),
		Status:                string(wager.StateRejected),
		PlayerID:              tx.PlayerID(),
		WalletID:              tx.WalletID(),
		ProviderID:            tx.ProviderID(),
		ExternalTransactionID: tx.ExternalID(),
		Money:                 toMoneyValue(tx.Amount()),
		FailureCode:           tx.FailureCode(),
		FailureMessage:        tx.FailureMessage(),
		RejectedAt:            tx.ProcessedAt(),
	})
}

// PendingReferencePayload é o conteúdo de WagerTransactionPendingReference.
type PendingReferencePayload struct {
	TransactionID                string     `json:"transactionId"`
	Kind                         string     `json:"kind"`
	Status                       string     `json:"status"`
	WalletID                     string     `json:"walletId"`
	ProviderID                   string     `json:"providerId,omitempty"`
	ExternalTransactionID        string     `json:"externalTransactionId,omitempty"`
	Money                        moneyValue `json:"money"`
	ReferenceExternalTransaction string     `json:"referenceExternalTransactionId"`
	NextAttemptAt                *time.Time `json:"nextAttemptAt,omitempty"`
}

// WagerTransactionPendingReference monta o payload da espera pela referência.
func WagerTransactionPendingReference(tx *wager.Transaction) ([]byte, error) {
	return json.Marshal(PendingReferencePayload{
		TransactionID:                tx.ID(),
		Kind:                         string(tx.Kind()),
		Status:                       string(wager.StatePendingReference),
		WalletID:                     tx.WalletID(),
		ProviderID:                   tx.ProviderID(),
		ExternalTransactionID:        tx.ExternalID(),
		Money:                        toMoneyValue(tx.Amount()),
		ReferenceExternalTransaction: tx.ReferenceExternalID(),
		NextAttemptAt:                pTimePtr(tx.ReferenceNextAttempt()),
	})
}

// BalanceChangedPayload é o conteúdo de WalletBalanceChanged.
type BalanceChangedPayload struct {
	WalletID      string     `json:"walletId"`
	TransactionID string     `json:"transactionId"`
	Direction     string     `json:"direction"`
	Money         moneyValue `json:"money"`
	BalanceBefore moneyValue `json:"balanceBefore"`
	BalanceAfter  moneyValue `json:"balanceAfter"`
	WalletVersion int64      `json:"walletVersion"`
}

// WalletBalanceChanged monta o payload da mudança de saldo.
//
// walletVersion é a versão da carteira após a mutação: começa em 1 na
// abertura e avança a cada mudança de saldo, permitindo ao consumidor
// ordenar e detectar perdas de evento.
func WalletBalanceChanged(walletID, transactionID string, direction ledger.Direction, amount, before, after money.Money, walletVersion int64) ([]byte, error) {
	return json.Marshal(BalanceChangedPayload{
		WalletID:      walletID,
		TransactionID: transactionID,
		Direction:     string(direction),
		Money:         toMoneyValue(amount),
		BalanceBefore: toMoneyValue(before),
		BalanceAfter:  toMoneyValue(after),
		WalletVersion: walletVersion,
	})
}

// businessPayload é o conteúdo canônico do payload de entrada que participa
// do hash (§9).
//
// Exclui o id interno, a chave de idempotência, o hash anterior e o relógio:
// o mesmo conteúdo de negócio deve produzir o mesmo hash venha do HTTP ou de
// uma fila.
type businessPayload struct {
	ProviderID     string     `json:"providerId"`
	ExternalID     string     `json:"externalTransactionId"`
	PlayerID       string     `json:"playerId"`
	WalletID       string     `json:"walletId"`
	RoundID        string     `json:"roundId,omitempty"`
	GameID         string     `json:"gameId,omitempty"`
	Kind           string     `json:"kind"`
	Money          moneyValue `json:"money"`
	ReferenceExtID string     `json:"referenceExternalTransactionId,omitempty"`
}

// CanonicalHash devolve o hash SHA-256 do payload de negócio em forma
// canônica.
//
// A forma é determinística: mesma entrada, mesmo hash. A identidade da
// transação, a chave de idempotência e metadados de transporte ficam fora,
// para que o hash identifique o conteúdo e não a embalagem.
func CanonicalHash(params wager.ExternalParams) (string, error) {
	corpo := businessPayload{
		ProviderID:     params.ProviderID,
		ExternalID:     params.ExternalID,
		PlayerID:       params.PlayerID,
		WalletID:       params.WalletID,
		RoundID:        params.RoundID,
		GameID:         params.GameID,
		Kind:           string(params.Kind),
		Money:          toMoneyValue(params.Amount),
		ReferenceExtID: params.ReferenceExtID,
	}
	canonico, err := json.Marshal(corpo)
	if err != nil {
		return "", fmt.Errorf("hash canonico: %w", err)
	}
	soma := sha256.Sum256(canonico)
	return hex.EncodeToString(soma[:]), nil
}

func pTimePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
