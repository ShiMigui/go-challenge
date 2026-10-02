package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/application/wallet"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	domainwallet "github.com/shimigui/go-challenge/internal/domain/wallet"
	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
	"github.com/shimigui/go-challenge/internal/interfaces/http/handler"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

// walletServiceStub implementa wallet.Service com comportamento configurável.
type walletServiceStub struct {
	createWallet func(ctx context.Context, playerID string, opening money.Money, now wallet.NowFunc) (*domainwallet.Wallet, error)
	getWallet    func(ctx context.Context, id string) (*domainwallet.Wallet, error)
	getLedger    func(ctx context.Context, walletID string, cursor *string, limit int) ([]*ledger.Entry, *string, error)
	reconcile    func(ctx context.Context, walletID string) (*wallet.Reconciliation, error)
}

func (s *walletServiceStub) CreateWallet(ctx context.Context, playerID string, opening money.Money, now wallet.NowFunc) (*domainwallet.Wallet, error) {
	if s.createWallet == nil {
		return nil, errors.New("stub CreateWallet não configurado")
	}
	return s.createWallet(ctx, playerID, opening, now)
}

func (s *walletServiceStub) GetWallet(ctx context.Context, id string) (*domainwallet.Wallet, error) {
	if s.getWallet == nil {
		return nil, errors.New("stub GetWallet não configurado")
	}
	return s.getWallet(ctx, id)
}

func (s *walletServiceStub) GetLedger(ctx context.Context, walletID string, cursor *string, limit int) ([]*ledger.Entry, *string, error) {
	if s.getLedger == nil {
		return nil, nil, errors.New("stub GetLedger não configurado")
	}
	return s.getLedger(ctx, walletID, cursor, limit)
}

func (s *walletServiceStub) Reconcile(ctx context.Context, walletID string) (*wallet.Reconciliation, error) {
	if s.reconcile == nil {
		return nil, errors.New("stub Reconcile não configurado")
	}
	return s.reconcile(ctx, walletID)
}

const carteiraUUID = "11111111-1111-4111-8111-111111111111"

func carteiraPronta(opening money.Money) *domainwallet.Wallet {
	w, err := domainwallet.New(domainwallet.Params{
		ID:       carteiraUUID,
		PlayerID: "player-1",
		Opening:  opening,
		Now:      time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		panic(err)
	}
	return w
}

func decodeWallet(t *testing.T, rr *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), dst); err != nil {
		t.Fatalf("corpo inválido: %v (%s)", err, rr.Body.String())
	}
}

// ===== POST /wallets =====

func TestCreateWalletValido(t *testing.T) {
	stub := &walletServiceStub{
		createWallet: func(ctx context.Context, playerID string, opening money.Money, now wallet.NowFunc) (*domainwallet.Wallet, error) {
			if playerID != "player-1" {
				t.Errorf("playerId = %q", playerID)
			}
			if opening.String() != "100.00" {
				t.Errorf("opening = %s", opening)
			}
			return carteiraPronta(opening), nil
		},
	}
	h := handler.NewWalletHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/wallets",
		strings.NewReader(`{"playerId":"player-1","initialBalance":{"amount":"100.00","currency":"BRL"}}`))

	h.CreateWallet(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, quer 201. Corpo: %s", rr.Code, rr.Body.String())
	}
	var resp dto.CreateWalletResponse
	decodeWallet(t, rr, &resp)
	if resp.ID != carteiraUUID {
		t.Errorf("id = %q", resp.ID)
	}
	if resp.PlayerID != "player-1" || resp.Version != 1 {
		t.Errorf("player/version = %q/%d", resp.PlayerID, resp.Version)
	}
	if resp.Balance.Amount != "100.00" || resp.Balance.Currency != "BRL" {
		t.Errorf("balance = %+v", resp.Balance)
	}
}

func TestCreateWalletJSONInvalido(t *testing.T) {
	h := handler.NewWalletHandler(&walletServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"playerId":`))

	h.CreateWallet(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("JSON inválido: status = %d, quer 400", rr.Code)
	}
	var resp dto.ErrorResponse
	decodeWallet(t, rr, &resp)
	if resp.Code != dto.ErrCodeInvalidInput {
		t.Errorf("code = %q", resp.Code)
	}
}

func TestCreateWalletSaldoMalFormado(t *testing.T) {
	// Escala excedente: entrada errada tem que ser 400, nunca 500.
	h := handler.NewWalletHandler(&walletServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/wallets",
		strings.NewReader(`{"playerId":"player-1","initialBalance":{"amount":"100.000","currency":"BRL"}}`))

	h.CreateWallet(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("saldo malformado: status = %d, quer 400. Corpo: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateWalletMoedaInexistente(t *testing.T) {
	h := handler.NewWalletHandler(&walletServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/wallets",
		strings.NewReader(`{"playerId":"player-1","initialBalance":{"amount":"100.00","currency":"XYZ"}}`))

	h.CreateWallet(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("moeda inexistente: status = %d, quer 400", rr.Code)
	}
}

func TestCreateWalletCarteiraDuplicada(t *testing.T) {
	stub := &walletServiceStub{
		createWallet: func(ctx context.Context, playerID string, opening money.Money, now wallet.NowFunc) (*domainwallet.Wallet, error) {
			return nil, domainwallet.ErrDuplicateWallet
		},
	}
	h := handler.NewWalletHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/wallets",
		strings.NewReader(`{"playerId":"player-1","initialBalance":{"amount":"0.00","currency":"BRL"}}`))

	h.CreateWallet(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicada: status = %d, quer 409", rr.Code)
	}
	var resp dto.ErrorResponse
	decodeWallet(t, rr, &resp)
	if resp.Code != dto.ErrCodeConflict {
		t.Errorf("code = %q", resp.Code)
	}
}

// ===== GET /wallets/{walletId} =====

func TestGetWalletValido(t *testing.T) {
	stub := &walletServiceStub{
		getWallet: func(ctx context.Context, id string) (*domainwallet.Wallet, error) {
			if id != carteiraUUID {
				t.Errorf("id = %q", id)
			}
			return carteiraPronta(money.MustParse("75.00", money.BRL)), nil
		},
	}
	h := handler.NewWalletHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+carteiraUUID, nil)
	req.SetPathValue("walletId", carteiraUUID)

	h.GetWallet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	var resp dto.WalletResponse
	decodeWallet(t, rr, &resp)
	if resp.Balance.Amount != "75.00" || resp.Version != 1 {
		t.Errorf("balance/version = %+v/%d", resp.Balance, resp.Version)
	}
}

func TestGetWalletSemWalletId(t *testing.T) {
	h := handler.NewWalletHandler(&walletServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wallets/", nil)

	h.GetWallet(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("sem walletId: status = %d, quer 400", rr.Code)
	}
}

func TestGetWalletNaoEncontrado(t *testing.T) {
	stub := &walletServiceStub{
		getWallet: func(ctx context.Context, id string) (*domainwallet.Wallet, error) {
			return nil, domainwallet.ErrWalletNotFound
		},
	}
	h := handler.NewWalletHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wallets/zzz", nil)
	req.SetPathValue("walletId", "zzz")

	h.GetWallet(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quer 404", rr.Code)
	}
	var resp dto.ErrorResponse
	decodeWallet(t, rr, &resp)
	if resp.Code != dto.ErrCodeNotFound {
		t.Errorf("code = %q", resp.Code)
	}
}

// ===== GET /wallets/{walletId}/ledger =====

func TestGetLedgerValido(t *testing.T) {
	entry := func(id string, direction ledger.Direction, valor, antes, depois string) *ledger.Entry {
		e, err := ledger.New(ledger.Params{
			ID:            id,
			WalletID:      carteiraUUID,
			TransactionID: "tx-" + id,
			Direction:     direction,
			Amount:        money.MustParse(valor, money.BRL),
			BalanceBefore: money.MustParse(antes, money.BRL),
			BalanceAfter:  money.MustParse(depois, money.BRL),
			Now:           time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("lancamento: %v", err)
		}
		return e
	}

	stub := &walletServiceStub{
		getLedger: func(ctx context.Context, walletID string, cursor *string, limit int) ([]*ledger.Entry, *string, error) {
			if walletID != carteiraUUID || limit != 50 {
				t.Errorf("walletID/limit = %q/%d", walletID, limit)
			}
			next := "proxima-pagina"
			return []*ledger.Entry{
				entry("e-2", ledger.Debit, "25.00", "100.00", "75.00"),
				entry("e-1", ledger.Credit, "100.00", "0.00", "100.00"),
			}, &next, nil
		},
	}
	h := handler.NewWalletHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+carteiraUUID+"/ledger", nil)
	req.SetPathValue("walletId", carteiraUUID)

	h.GetLedger(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	var resp dto.LedgerPageResponse
	decodeWallet(t, rr, &resp)
	if len(resp.Entries) != 2 {
		t.Fatalf("entradas = %d, quer 2", len(resp.Entries))
	}
	if resp.Entries[0].Direction != "DEBIT" || resp.Entries[0].BalanceAfter.Amount != "75.00" {
		t.Errorf("primeira entrada = %+v", resp.Entries[0])
	}
	if resp.NextCursor == nil || *resp.NextCursor != "proxima-pagina" {
		t.Errorf("nextCursor = %v", resp.NextCursor)
	}
	if resp.Limit != 50 {
		t.Errorf("limit = %d, quer 50", resp.Limit)
	}
}

func TestGetLedgerSemWalletId(t *testing.T) {
	h := handler.NewWalletHandler(&walletServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wallets//ledger", nil)

	h.GetLedger(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("sem walletId: status = %d, quer 400", rr.Code)
	}
}

// ===== GET /wallets/{walletId}/reconciliation =====

func TestReconcileConsistente(t *testing.T) {
	stub := &walletServiceStub{
		reconcile: func(ctx context.Context, walletID string) (*wallet.Reconciliation, error) {
			if walletID != carteiraUUID {
				t.Errorf("walletID = %q", walletID)
			}
			return &wallet.Reconciliation{
				WalletID:          walletID,
				StoredBalance:     money.MustParse("75.00", money.BRL),
				CalculatedBalance: money.MustParse("75.00", money.BRL),
				Difference:        money.MustParse("0.00", money.BRL),
				Consistent:        true,
				CheckedEntries:    2,
			}, nil
		},
	}
	h := handler.NewWalletHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wallets/"+carteiraUUID+"/reconciliation", nil)
	req.SetPathValue("walletId", carteiraUUID)

	h.Reconcile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	var resp dto.ReconciliationResponse
	decodeWallet(t, rr, &resp)
	if !resp.Consistent || resp.CheckedEntries != 2 {
		t.Errorf("consistent/checked = %v/%d", resp.Consistent, resp.CheckedEntries)
	}
	if resp.StoredBalance.Amount != "75.00" || resp.Difference.Amount != "0.00" {
		t.Errorf("saldos = %+v / %+v", resp.StoredBalance, resp.Difference)
	}
}

func TestReconcileInexistente(t *testing.T) {
	stub := &walletServiceStub{
		reconcile: func(ctx context.Context, walletID string) (*wallet.Reconciliation, error) {
			return nil, domainwallet.ErrWalletNotFound
		},
	}
	h := handler.NewWalletHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/wallets/x/reconciliation", nil)
	req.SetPathValue("walletId", "x")

	h.Reconcile(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quer 404", rr.Code)
	}
}

// O middleware de idempotência também vale para a criação: sem a chave a
// mutação é recusada antes do handler.
func TestCreateWalletViaChainExigeIdempotency(t *testing.T) {
	h := handler.NewWalletHandler(&walletServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/wallets",
		strings.NewReader(`{"playerId":"player-1","initialBalance":{"amount":"0.00","currency":"BRL"}}`))

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.CreateWallet)).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("sem chave: status = %d, quer 400", rr.Code)
	}
}
