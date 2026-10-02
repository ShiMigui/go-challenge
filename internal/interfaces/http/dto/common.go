package dto

import (
	"errors"
	"fmt"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

// MoneyPayload represents a monetary value in API contracts.
type MoneyPayload struct {
	Amount   string `json:"amount"`   // "25.00"
	Currency string `json:"currency"` // "BRL"
}

const (
	ErrCodeInvalidInput        = "INVALID_INPUT"
	ErrCodeConflict            = "CONFLICT"
	ErrCodeNotFound            = "NOT_FOUND"
	ErrCodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	ErrCodeInsufficientFunds   = "INSUFFICIENT_FUNDS"
	ErrCodeRejected            = "REJECTED"
	ErrCodePending             = "PENDING"
	ErrCodeUnavailable         = "UNAVAILABLE"
	ErrCodeUnauthorized        = "UNAUTHORIZED"
	ErrCodeForbidden           = "FORBIDDEN"
)

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

type HealthResponse struct {
	Status    string            `json:"status"`
	Checks    map[string]string `json:"checks,omitempty"`
	Timestamp string            `json:"timestamp,omitempty"`
}

type DomainError struct {
	Err        error
	StatusCode int
	Code       string
}

func (e *DomainError) Error() string { return e.Err.Error() }
func (e *DomainError) Unwrap() error { return e.Err }

func NewDomainError(err error, statusCode int, code string) *DomainError {
	return &DomainError{Err: err, StatusCode: statusCode, Code: code}
}

func ErrInvalidInput(err error) *DomainError {
	return NewDomainError(err, 400, ErrCodeInvalidInput)
}

func ErrConflict(err error) *DomainError {
	return NewDomainError(err, 409, ErrCodeConflict)
}

func ErrNotFound(err error) *DomainError {
	return NewDomainError(err, 404, ErrCodeNotFound)
}

func ErrIdempotencyConflictError(err error) *DomainError {
	return NewDomainError(err, 409, ErrCodeIdempotencyConflict)
}

func ErrInsufficientFundsError(err error) *DomainError {
	return NewDomainError(err, 409, ErrCodeInsufficientFunds)
}

func ErrRejected(err error) *DomainError {
	return NewDomainError(err, 422, ErrCodeRejected)
}

func ErrPending(err error) *DomainError {
	return NewDomainError(err, 202, ErrCodePending)
}

func ErrUnavailable(err error) *DomainError {
	return NewDomainError(err, 503, ErrCodeUnavailable)
}

func ErrUnauthorized(err error) *DomainError {
	return NewDomainError(err, 401, ErrCodeUnauthorized)
}

func ErrForbidden(err error) *DomainError {
	return NewDomainError(err, 403, ErrCodeForbidden)
}

func ErrInternal(err error) *DomainError {
	return NewDomainError(err, 500, "INTERNAL_ERROR")
}

func MapDomainError(err error) *DomainError {
	if err == nil {
		return nil
	}

	var de *DomainError
	if errors.As(err, &de) {
		return de
	}

	switch {
	case errors.Is(err, wallet.ErrWalletNotFound):
		return ErrNotFound(err)
	case errors.Is(err, wager.ErrTransactionNotFound):
		return ErrNotFound(err)
	case errors.Is(err, wallet.ErrDuplicateWallet):
		return ErrConflict(err)
	case errors.Is(err, wager.ErrDuplicate):
		return ErrConflict(err)
	case errors.Is(err, wallet.ErrInsufficientFunds):
		return ErrInsufficientFundsError(err)
	case errors.Is(err, wager.ErrIdempotencyConflict):
		return ErrIdempotencyConflictError(err)
	case errors.Is(err, money.ErrCurrencyMismatch):
		return ErrInvalidInput(err)
	case errors.Is(err, money.ErrNegativeAmount):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrInvalidAmountForKind):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrMissingReference):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrForbiddenReference):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrProviderMismatch):
		return ErrForbidden(err)
	case errors.Is(err, wager.ErrPlayerMismatch):
		return ErrForbidden(err)
	case errors.Is(err, wager.ErrCurrencyMismatchOnReference):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrAmountMismatchOnReference):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrReferenceNotProcessed):
		return ErrRejected(err)
	case errors.Is(err, wager.ErrInvalidKind):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrInvalidStateTransition):
		return ErrRejected(err)
	case errors.Is(err, wager.ErrTerminalTransition):
		return ErrRejected(err)
	case errors.Is(err, wager.ErrFailureCodeRequired):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrExternalNotAllowed):
		return ErrInvalidInput(err)
	case errors.Is(err, identifier.ErrInvalidPlayerID):
		return ErrInvalidInput(err)
	case errors.Is(err, identifier.ErrInvalidWalletID):
		return ErrInvalidInput(err)
	case errors.Is(err, identifier.ErrInvalidTransactionID):
		return ErrInvalidInput(err)
	case errors.Is(err, identifier.ErrInvalidID):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrInvalidProviderID):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrInvalidExternalID):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrInvalidIdempotencyKey):
		return ErrInvalidInput(err)
	case errors.Is(err, wager.ErrInvalidPayloadHash):
		return ErrInvalidInput(err)
	case errors.Is(err, ledger.ErrNonPositiveAmount):
		return ErrInvalidInput(err)
	case errors.Is(err, ledger.ErrBalanceArithmetic):
		return ErrInternal(err)
	case errors.Is(err, ledger.ErrInvalidDirection):
		return ErrInvalidInput(err)
	}

	return ErrInternal(fmt.Errorf("unhandled: %w", err))
}
