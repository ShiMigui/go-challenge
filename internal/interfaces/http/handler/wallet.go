package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/shimigui/go-challenge/internal/application/wallet"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

type WalletHandler struct {
	svc wallet.Service
}

func NewWalletHandler(svc wallet.Service) *WalletHandler {
	return &WalletHandler{svc: svc}
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateWalletRequest
	if err := middleware.DecodeJSON(r, &req); err != nil {
		middleware.RespondError(w, r, dto.ErrInvalidInput(err))
		return
	}

	opening, err := money.Parse(req.InitialBalance.Amount, money.Currency(req.InitialBalance.Currency))
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	wlt, err := h.svc.CreateWallet(r.Context(), req.PlayerID, opening, time.Now)
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	middleware.RespondJSON(w, r, http.StatusCreated, dto.CreateWalletResponse{
		ID:       wlt.ID(),
		PlayerID: wlt.PlayerID(),
		Balance:  dto.MoneyPayload{Amount: wlt.Balance().String(), Currency: string(wlt.Currency())},
		Version:  wlt.Version(),
	})
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("walletId")
	if id == "" {
		middleware.RespondError(w, r, dto.ErrInvalidInput(errors.New("walletId required")))
		return
	}

	wlt, err := h.svc.GetWallet(r.Context(), id)
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	middleware.RespondJSON(w, r, http.StatusOK, dto.WalletResponse{
		ID:       wlt.ID(),
		PlayerID: wlt.PlayerID(),
		Balance:  dto.MoneyPayload{Amount: wlt.Balance().String(), Currency: string(wlt.Currency())},
		Version:  wlt.Version(),
	})
}

func (h *WalletHandler) GetLedger(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("walletId")
	if walletID == "" {
		middleware.RespondError(w, r, dto.ErrInvalidInput(errors.New("walletId required")))
		return
	}

	cursor := r.URL.Query().Get("cursor")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	var c *string
	if cursor != "" {
		c = &cursor
	}

	entries, nextCursor, err := h.svc.GetLedger(r.Context(), walletID, c, limit)
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	resp := dto.LedgerPageResponse{
		Entries:    make([]dto.LedgerEntryResponse, len(entries)),
		NextCursor: nextCursor,
		Limit:      limit,
	}
	for i, e := range entries {
		resp.Entries[i] = dto.LedgerEntryResponse{
			ID:            e.ID(),
			TransactionID: e.TransactionID(),
			Direction:     string(e.Direction()),
			Amount:        dto.MoneyPayload{Amount: e.Amount().String(), Currency: string(e.Amount().Currency())},
			BalanceBefore: dto.MoneyPayload{Amount: e.BalanceBefore().String(), Currency: string(e.BalanceBefore().Currency())},
			BalanceAfter:  dto.MoneyPayload{Amount: e.BalanceAfter().String(), Currency: string(e.BalanceAfter().Currency())},
			CreatedAt:     e.CreatedAt(),
		}
	}
	middleware.RespondJSON(w, r, http.StatusOK, resp)
}

func (h *WalletHandler) Reconcile(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("walletId")
	if walletID == "" {
		middleware.RespondError(w, r, dto.ErrInvalidInput(errors.New("walletId required")))
		return
	}

	rec, err := h.svc.Reconcile(r.Context(), walletID)
	if err != nil {
		middleware.RespondError(w, r, dto.MapDomainError(err))
		return
	}

	middleware.RespondJSON(w, r, http.StatusOK, dto.ReconciliationResponse{
		WalletID:          rec.WalletID,
		StoredBalance:     dto.MoneyPayload{Amount: rec.StoredBalance.String(), Currency: string(rec.StoredBalance.Currency())},
		CalculatedBalance: dto.MoneyPayload{Amount: rec.CalculatedBalance.String(), Currency: string(rec.CalculatedBalance.Currency())},
		Difference:        dto.MoneyPayload{Amount: rec.Difference.String(), Currency: string(rec.Difference.Currency())},
		Consistent:        rec.Consistent,
		CheckedEntries:    rec.CheckedEntries,
	})
}
