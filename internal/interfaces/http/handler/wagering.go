package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/shimigui/go-challenge/internal/application/wagering"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

type WageringHandler struct {
	svc wagering.Service
}

func NewWageringHandler(svc wagering.Service) *WageringHandler {
	return &WageringHandler{svc: svc}
}

func (h *WageringHandler) SubmitTransaction(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateTransactionRequest
	if err := middleware.DecodeJSON(r, &req); err != nil {
		middleware.RespondError(w, r, dto.ErrInvalidInput(err))
		return
	}

	idempotencyKey := middleware.GetIdempotencyKey(r)
	if idempotencyKey == "" {
		middleware.RespondError(w, r, dto.ErrInvalidInput(errors.New("Idempotency-Key required")))
		return
	}

	amt, err := money.Parse(req.Money.Amount, money.Currency(req.Money.Currency))
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	params := wager.ExternalParams{
		ID:             identifier.New(),
		ProviderID:     req.ProviderID,
		ExternalID:     req.ExternalTransactionID,
		IdempotencyKey: idempotencyKey,
		PayloadHash:    "placeholder-hash",
		PlayerID:       req.PlayerID,
		WalletID:       req.WalletID,
		Kind:           wager.Kind(req.Kind),
		Amount:         amt,
		RoundID:        req.RoundID,
		GameID:         req.GameID,
		ReferenceExtID: req.ReferenceExternalTransactionID,
		Now:            time.Now(),
	}

	res, err := h.svc.SubmitTransaction(r.Context(), params)
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	middleware.RespondJSON(w, r, http.StatusOK, dto.CreateTransactionResponse{
		TransactionID:    res.Transaction.ID(),
		Status:           dto.TransactionState(res.Transaction.State()),
		Balance:          dto.MoneyPayload{Amount: res.Transaction.Amount().String(), Currency: string(res.Transaction.Currency())},
		IdempotentReplay: res.Replayed,
	})
}

func (h *WageringHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("transactionId")
	if id == "" {
		middleware.RespondError(w, r, dto.ErrInvalidInput(errors.New("transactionId required")))
		return
	}

	tx, err := h.svc.GetTransaction(r.Context(), id)
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	middleware.RespondJSON(w, r, http.StatusOK, toTransactionResponse(tx))
}

func (h *WageringHandler) GetTransactionByExternal(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("providerId")
	externalID := r.PathValue("externalTransactionId")
	if providerID == "" || externalID == "" {
		middleware.RespondError(w, r, dto.ErrInvalidInput(errors.New("providerId and externalTransactionId required")))
		return
	}

	tx, err := h.svc.GetTransactionByExternal(r.Context(), providerID, externalID)
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	middleware.RespondJSON(w, r, http.StatusOK, toTransactionResponse(tx))
}

func toTransactionResponse(tx *wager.Transaction) dto.TransactionResponse {
	resp := dto.TransactionResponse{
		TransactionID:                  tx.ID(),
		Kind:                           dto.TransactionKind(tx.Kind()),
		State:                          dto.TransactionState(tx.State()),
		PlayerID:                       tx.PlayerID(),
		WalletID:                       tx.WalletID(),
		RoundID:                        tx.RoundID(),
		GameID:                         tx.GameID(),
		ProviderID:                     tx.ProviderID(),
		ExternalTransactionID:          tx.ExternalID(),
		Money:                          dto.MoneyPayload{Amount: tx.Amount().String(), Currency: string(tx.Currency())},
		ReferenceExternalTransactionID: tx.ReferenceExternalID(),
		ReferenceTransactionID:         tx.ReferenceID(),
		FailureCode:                    tx.FailureCode(),
		FailureMessage:                 tx.FailureMessage(),
		ReferenceAttempts:              tx.ReferenceAttempts(),
		ReferenceNextAttempt:           nilPtr(tx.ReferenceNextAttempt()),
		CreatedAt:                      tx.CreatedAt(),
		UpdatedAt:                      tx.UpdatedAt(),
		ProcessedAt:                    nilPtr(tx.ProcessedAt()),
	}
	return resp
}

func nilPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
