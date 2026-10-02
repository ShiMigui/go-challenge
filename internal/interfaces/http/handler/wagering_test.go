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

	"github.com/shimigui/go-challenge/internal/application/wagering"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	domainwallet "github.com/shimigui/go-challenge/internal/domain/wallet"
	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
	"github.com/shimigui/go-challenge/internal/interfaces/http/handler"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

// wageringServiceStub implementa wagering.Service com comportamento
// configurável por teste.
type wageringServiceStub struct {
	submit   func(ctx context.Context, params wager.ExternalParams) (*wagering.SubmitResult, error)
	get      func(ctx context.Context, id string) (*wager.Transaction, error)
	getByExt func(ctx context.Context, providerID, externalID string) (*wager.Transaction, error)
}

func (s *wageringServiceStub) SubmitTransaction(ctx context.Context, params wager.ExternalParams) (*wagering.SubmitResult, error) {
	if s.submit == nil {
		return nil, errors.New("stub SubmitTransaction não configurado")
	}
	return s.submit(ctx, params)
}

func (s *wageringServiceStub) GetTransaction(ctx context.Context, id string) (*wager.Transaction, error) {
	if s.get == nil {
		return nil, errors.New("stub GetTransaction não configurado")
	}
	return s.get(ctx, id)
}

func (s *wageringServiceStub) GetTransactionByExternal(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	if s.getByExt == nil {
		return nil, errors.New("stub GetTransactionByExternal não configurado")
	}
	return s.getByExt(ctx, providerID, externalID)
}

const transacaoUUID = "22222222-2222-4222-8222-222222222222"

func transacaoProcessada() *wager.Transaction {
	tx, err := wager.NewExternal(wager.ExternalParams{
		ID:             transacaoUUID,
		ProviderID:     "provider-1",
		ExternalID:     "ext-1",
		IdempotencyKey: "key-1",
		PayloadHash:    "hash-1",
		PlayerID:       "player-1",
		WalletID:       carteiraUUID,
		Kind:           wager.KindBet,
		Amount:         money.MustParse("25.00", money.BRL),
		RoundID:        "round-1",
		GameID:         "game-1",
		Now:            time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		panic(err)
	}
	if err := tx.MarkProcessed(time.Date(2026, 10, 1, 12, 0, 5, 0, time.UTC)); err != nil {
		panic(err)
	}
	return tx
}

func decodeBody(t *testing.T, rr *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), dst); err != nil {
		t.Fatalf("corpo inválido: %v (%s)", err, rr.Body.String())
	}
}

const submitBetBody = `{
	"providerId":"provider-1",
	"externalTransactionId":"ext-1",
	"playerId":"player-1",
	"walletId":"11111111-1111-4111-8111-111111111111",
	"roundId":"round-1",
	"gameId":"game-1",
	"kind":"BET",
	"money":{"amount":"25.00","currency":"BRL"}
}`

// ===== POST /transactions =====

func TestSubmitTransactionValida(t *testing.T) {
	stub := &wageringServiceStub{
		submit: func(ctx context.Context, params wager.ExternalParams) (*wagering.SubmitResult, error) {
			if params.ProviderID != "provider-1" || params.Kind != wager.KindBet {
				t.Errorf("params = %+v", params)
			}
			if params.IdempotencyKey != "chave-1" {
				t.Errorf("idempotencyKey = %q", params.IdempotencyKey)
			}
			return &wagering.SubmitResult{Transaction: transacaoProcessada(), Replayed: false}, nil
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(submitBetBody))
	req.Header.Set("Idempotency-Key", "chave-1")

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.SubmitTransaction)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200. Corpo: %s", rr.Code, rr.Body.String())
	}
	var resp dto.CreateTransactionResponse
	decodeBody(t, rr, &resp)
	if resp.TransactionID != transacaoUUID {
		t.Errorf("transactionId = %q", resp.TransactionID)
	}
	if resp.Status != dto.StateProcessed {
		t.Errorf("status = %q, quer PROCESSED", resp.Status)
	}
	if resp.IdempotentReplay {
		t.Error("primeira submissão não pode ser replay")
	}
	if resp.Balance.Amount != "25.00" || resp.Balance.Currency != "BRL" {
		t.Errorf("balance = %+v", resp.Balance)
	}
}

func TestSubmitTransactionSemChaveDeIdempotencia(t *testing.T) {
	h := handler.NewWageringHandler(&wageringServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(submitBetBody))

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.SubmitTransaction)).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("sem chave: status = %d, quer 400", rr.Code)
	}
	var resp dto.ErrorResponse
	decodeBody(t, rr, &resp)
	if resp.Code != dto.ErrCodeInvalidInput {
		t.Errorf("code = %q", resp.Code)
	}
}

func TestSubmitTransactionJSONInvalido(t *testing.T) {
	h := handler.NewWageringHandler(&wageringServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(`{"kind":`))
	req.Header.Set("Idempotency-Key", "k")

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.SubmitTransaction)).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("JSON inválido: status = %d, quer 400", rr.Code)
	}
}

func TestSubmitTransactionValorMonetarioInvalido(t *testing.T) {
	h := handler.NewWageringHandler(&wageringServiceStub{})

	body := strings.Replace(submitBetBody, `"25.00"`, `"25.0"`, 1)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(body))
	req.Header.Set("Idempotency-Key", "k")

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.SubmitTransaction)).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("valor malformado: status = %d, quer 400. Corpo: %s", rr.Code, rr.Body.String())
	}
}

func TestSubmitTransactionSaldoInsuficiente(t *testing.T) {
	stub := &wageringServiceStub{
		submit: func(ctx context.Context, params wager.ExternalParams) (*wagering.SubmitResult, error) {
			return nil, domainwallet.ErrInsufficientFunds
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(submitBetBody))
	req.Header.Set("Idempotency-Key", "k")

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.SubmitTransaction)).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, quer 409", rr.Code)
	}
	var resp dto.ErrorResponse
	decodeBody(t, rr, &resp)
	if resp.Code != dto.ErrCodeInsufficientFunds {
		t.Errorf("code = %q", resp.Code)
	}
}

func TestSubmitTransactionReentrega(t *testing.T) {
	tx := transacaoProcessada()
	stub := &wageringServiceStub{
		submit: func(ctx context.Context, params wager.ExternalParams) (*wagering.SubmitResult, error) {
			return &wagering.SubmitResult{Transaction: tx, Replayed: true}, nil
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(submitBetBody))
	req.Header.Set("Idempotency-Key", "k")

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.SubmitTransaction)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	var resp dto.CreateTransactionResponse
	decodeBody(t, rr, &resp)
	if !resp.IdempotentReplay {
		t.Error("reentrega deveria marcar idempotentReplay=true")
	}
	if resp.TransactionID != transacaoUUID {
		t.Errorf("transactionId = %q", resp.TransactionID)
	}
}

func TestSubmitTransactionReversaoInvalidaRetorna400(t *testing.T) {
	// ROLLBACK sem referência resolvida recusa o movimento — o contrato de
	// erro é 400 (INVALID_INPUT), não 500.
	stub := &wageringServiceStub{
		submit: func(ctx context.Context, params wager.ExternalParams) (*wagering.SubmitResult, error) {
			return nil, wager.ErrInvalidKind
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/transactions", strings.NewReader(submitBetBody))
	req.Header.Set("Idempotency-Key", "k")

	middleware.IdempotencyMiddleware(http.HandlerFunc(h.SubmitTransaction)).ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quer 400", rr.Code)
	}
}

// ===== GET /transactions/{transactionId} =====

func TestGetTransactionValida(t *testing.T) {
	stub := &wageringServiceStub{
		get: func(ctx context.Context, id string) (*wager.Transaction, error) {
			if id != transacaoUUID {
				t.Errorf("id = %q", id)
			}
			return transacaoProcessada(), nil
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/transactions/"+transacaoUUID, nil)
	req.SetPathValue("transactionId", transacaoUUID)

	h.GetTransaction(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	var resp dto.TransactionResponse
	decodeBody(t, rr, &resp)
	if resp.TransactionID != transacaoUUID || resp.Kind != dto.KindBet || resp.State != dto.StateProcessed {
		t.Errorf("resposta = %+v", resp)
	}
	if resp.Money.Amount != "25.00" || resp.PlayerID != "player-1" {
		t.Errorf("money/player = %+v / %q", resp.Money, resp.PlayerID)
	}
	if resp.ProcessedAt == nil || resp.ProcessedAt.IsZero() {
		t.Error("processedAt deveria estar presente")
	}
}

func TestGetTransactionSemId(t *testing.T) {
	h := handler.NewWageringHandler(&wageringServiceStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/transactions/", nil)

	h.GetTransaction(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("sem id: status = %d, quer 400", rr.Code)
	}
}

func TestGetTransactionNaoEncontrada(t *testing.T) {
	stub := &wageringServiceStub{
		get: func(ctx context.Context, id string) (*wager.Transaction, error) {
			return nil, wager.ErrTransactionNotFound
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/transactions/zzz", nil)
	req.SetPathValue("transactionId", "zzz")

	h.GetTransaction(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quer 404", rr.Code)
	}
	var resp dto.ErrorResponse
	decodeBody(t, rr, &resp)
	if resp.Code != dto.ErrCodeNotFound {
		t.Errorf("code = %q", resp.Code)
	}
}

// ===== GET /providers/{providerId}/transactions/{externalTransactionId} =====

func TestGetTransactionByExternalValida(t *testing.T) {
	stub := &wageringServiceStub{
		getByExt: func(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
			if providerID != "provider-1" || externalID != "ext-1" {
				t.Errorf("provider/external = %q/%q", providerID, externalID)
			}
			return transacaoProcessada(), nil
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/provider-1/transactions/ext-1", nil)
	req.SetPathValue("providerId", "provider-1")
	req.SetPathValue("externalTransactionId", "ext-1")

	h.GetTransactionByExternal(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	var resp dto.TransactionResponse
	decodeBody(t, rr, &resp)
	if resp.ExternalTransactionID != "ext-1" {
		t.Errorf("externalTransactionId = %q", resp.ExternalTransactionID)
	}
}

func TestGetTransactionByExternalSemParametros(t *testing.T) {
	h := handler.NewWageringHandler(&wageringServiceStub{})

	t.Run("sem provedor", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/providers//transactions/ext-1", nil)
		req.SetPathValue("externalTransactionId", "ext-1")
		h.GetTransactionByExternal(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, quer 400", rr.Code)
		}
	})

	t.Run("sem id externo", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/providers/provider-1/transactions/", nil)
		req.SetPathValue("providerId", "provider-1")
		h.GetTransactionByExternal(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, quer 400", rr.Code)
		}
	})
}

func TestGetTransactionByExternalNaoEncontrada(t *testing.T) {
	stub := &wageringServiceStub{
		getByExt: func(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
			return nil, wager.ErrTransactionNotFound
		},
	}
	h := handler.NewWageringHandler(stub)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/p/transactions/e", nil)
	req.SetPathValue("providerId", "p")
	req.SetPathValue("externalTransactionId", "e")

	h.GetTransactionByExternal(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, quer 404", rr.Code)
	}
}
