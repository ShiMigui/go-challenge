package dto_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
)

// MapDomainError é o contrato entre erros de domínio/aplicação e a resposta
// HTTP. Cada uma destas linhas é um caso "errado" que deve sair pela porta
// certa (status + código).
func TestMapDomainError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"carteira não encontrada", wallet.ErrWalletNotFound, http.StatusNotFound, dto.ErrCodeNotFound},
		{"transação não encontrada", wager.ErrTransactionNotFound, http.StatusNotFound, dto.ErrCodeNotFound},
		{"carteira duplicada", wallet.ErrDuplicateWallet, http.StatusConflict, dto.ErrCodeConflict},
		{"transação duplicada (reentrega)", wager.ErrDuplicate, http.StatusConflict, dto.ErrCodeConflict},
		{"saldo insuficiente", wallet.ErrInsufficientFunds, http.StatusConflict, dto.ErrCodeInsufficientFunds},
		{"chave de idempotência reaproveitada", wager.ErrIdempotencyConflict, http.StatusConflict, dto.ErrCodeIdempotencyConflict},
		{"moedas incompatíveis", money.ErrCurrencyMismatch, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"valor negativo", money.ErrNegativeAmount, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"valor monetário inválido", money.ErrInvalidAmount, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"moeda inválida", money.ErrInvalidCurrency, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"formato monetário inválido", money.ErrInvalidFormat, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"valor incompatível com o tipo", wager.ErrInvalidAmountForKind, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"referência ausente", wager.ErrMissingReference, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"referência proibida", wager.ErrForbiddenReference, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"provedor difere da referência", wager.ErrProviderMismatch, http.StatusForbidden, dto.ErrCodeForbidden},
		{"jogador difere da referência", wager.ErrPlayerMismatch, http.StatusForbidden, dto.ErrCodeForbidden},
		{"moeda difere da referência", wager.ErrCurrencyMismatchOnReference, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"valor difere da referência", wager.ErrAmountMismatchOnReference, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"referência não processada", wager.ErrReferenceNotProcessed, http.StatusUnprocessableEntity, dto.ErrCodeRejected},
		{"tipo inválido", wager.ErrInvalidKind, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"transição inválida", wager.ErrInvalidStateTransition, http.StatusUnprocessableEntity, dto.ErrCodeRejected},
		{"transação terminal", wager.ErrTerminalTransition, http.StatusUnprocessableEntity, dto.ErrCodeRejected},
		{"código de falha ausente", wager.ErrFailureCodeRequired, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"OPENING vindo de fora", wager.ErrExternalNotAllowed, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"player_id obrigatório", identifier.ErrInvalidPlayerID, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"wallet_id obrigatório", identifier.ErrInvalidWalletID, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"transaction_id obrigatório", identifier.ErrInvalidTransactionID, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"identificador inválido", identifier.ErrInvalidID, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"provider_id obrigatório", wager.ErrInvalidProviderID, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"id externo obrigatório", wager.ErrInvalidExternalID, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"chave de idempotência vazia", wager.ErrInvalidIdempotencyKey, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"payload hash vazio", wager.ErrInvalidPayloadHash, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"lançamento não positivo", ledger.ErrNonPositiveAmount, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"direção de lançamento inválida", ledger.ErrInvalidDirection, http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{"aritmética de saldo quebrada", ledger.ErrBalanceArithmetic, http.StatusInternalServerError, dto.ErrCodeInternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			de := dto.MapDomainError(c.err)
			if de == nil {
				t.Fatal("MapDomainError devolveu nil para erro conhecido")
			}
			if de.StatusCode != c.wantStatus {
				t.Errorf("status = %d, quer %d", de.StatusCode, c.wantStatus)
			}
			if de.Code != c.wantCode {
				t.Errorf("code = %q, quer %q", de.Code, c.wantCode)
			}
			if !errors.Is(de, c.err) {
				t.Errorf("errors.Is(de, %v) falhou", c.err)
			}
		})
	}
}

// Erros sem mapeamento são falhas internas: 500, código INTERNAL_ERROR,
// preservando a causa em "unhandled".
func TestMapDomainErrorDesconhecidoViraInterno(t *testing.T) {
	causa := errors.New("alguma coisa inesperada")
	de := dto.MapDomainError(causa)
	if de.StatusCode != http.StatusInternalServerError || de.Code != dto.ErrCodeInternal {
		t.Fatalf("status=%d code=%s, quer 500/INTERNAL_ERROR", de.StatusCode, de.Code)
	}
	if !errors.Is(de, causa) {
		t.Error("causa original não preservada")
	}
}

func TestMapDomainErrorNil(t *testing.T) {
	if de := dto.MapDomainError(nil); de != nil {
		t.Fatalf("nil deveria virar nil, veio %v", de)
	}
}

// Um DomainError já pronto é devolvido como está (passagem de erros da
// infraestrutura para a resposta).
func TestMapDomainErrorPassaDomainErrorExistente(t *testing.T) {
	original := dto.NewDomainError(errors.New("sem chave"), http.StatusBadRequest, dto.ErrCodeInvalidInput)
	got := dto.MapDomainError(fmt.Errorf("wrap: %w", original))
	if got != original {
		t.Fatalf("deveria devolver o mesmo DomainError, veio %v", got)
	}
}

// Erros curto-circuitados por %w devem continuar reconhecidos.
func TestMapDomainErrorEmbrulhado(t *testing.T) {
	err := fmt.Errorf("contexto: %w", wallet.ErrWalletNotFound)
	de := dto.MapDomainError(err)
	if de.StatusCode != http.StatusNotFound || de.Code != dto.ErrCodeNotFound {
		t.Fatalf("erro embrulhado mapeado errado: %d/%s", de.StatusCode, de.Code)
	}
}

func TestConstrutoresDeErro(t *testing.T) {
	causa := errors.New("causa")
	cases := []struct {
		got    *dto.DomainError
		status int
		code   string
	}{
		{dto.ErrInvalidInput(causa), http.StatusBadRequest, dto.ErrCodeInvalidInput},
		{dto.ErrConflict(causa), http.StatusConflict, dto.ErrCodeConflict},
		{dto.ErrNotFound(causa), http.StatusNotFound, dto.ErrCodeNotFound},
		{dto.ErrIdempotencyConflictError(causa), http.StatusConflict, dto.ErrCodeIdempotencyConflict},
		{dto.ErrInsufficientFundsError(causa), http.StatusConflict, dto.ErrCodeInsufficientFunds},
		{dto.ErrRejected(causa), http.StatusUnprocessableEntity, dto.ErrCodeRejected},
		{dto.ErrPending(causa), http.StatusAccepted, dto.ErrCodePending},
		{dto.ErrUnavailable(causa), http.StatusServiceUnavailable, dto.ErrCodeUnavailable},
		{dto.ErrUnauthorized(causa), http.StatusUnauthorized, dto.ErrCodeUnauthorized},
		{dto.ErrForbidden(causa), http.StatusForbidden, dto.ErrCodeForbidden},
		{dto.ErrInternal(causa), http.StatusInternalServerError, dto.ErrCodeInternal},
	}
	for _, c := range cases {
		if c.got.StatusCode != c.status || c.got.Code != c.code {
			t.Errorf("esperava %d/%s, veio %d/%s", c.status, c.code, c.got.StatusCode, c.got.Code)
		}
		if c.got.Error() != "causa" {
			t.Errorf("Error() = %q, quer a causa", c.got.Error())
		}
	}
}

// ErrCodeInternal existe justamente para o mapeamento padrão de 500.
func TestErrCodeInternalDefinido(t *testing.T) {
	if dto.ErrCodeInternal != "INTERNAL_ERROR" {
		t.Errorf("ErrCodeInternal = %q", dto.ErrCodeInternal)
	}
}
