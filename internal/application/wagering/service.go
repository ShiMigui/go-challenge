// Package wagering contém os casos de uso de apostas.
//
// A camada de aplicação conhece apenas ports: o agregado do domínio e a
// transação provida pela infraestrutura.
package wagering

import (
	"context"
	"errors"
	"time"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
)

// SubmitResult devolve a transação respondida e se ela foi reentrega.
//
// Replayed=true significa que o provedor repetiu uma identidade já
// processada: a resposta é o registro anterior, sem reprocessar.
type SubmitResult struct {
	Transaction *wager.Transaction
	Replayed    bool
}

// Service define as operações de wagering que a interface HTTP usa.
type Service interface {
	SubmitTransaction(ctx context.Context, params wager.ExternalParams) (*SubmitResult, error)
	GetTransaction(ctx context.Context, id string) (*wager.Transaction, error)
	GetTransactionByExternal(ctx context.Context, providerID, externalID string) (*wager.Transaction, error)
}

// service implementa o caso de uso de wagering.
type service struct {
	wagerRepo wager.WagerTransactionRepository
	txManager ports.TransactionManager
}

// NewService monta o caso de uso com as portas de saída.
func NewService(
	wagerRepo wager.WagerTransactionRepository,
	txManager ports.TransactionManager,
) Service {
	return &service{
		wagerRepo: wagerRepo,
		txManager: txManager,
	}
}

// SubmitTransaction processa a operação do provedor dentro da transação.
func (s *service) SubmitTransaction(ctx context.Context, params wager.ExternalParams) (*SubmitResult, error) {
	var resultado *SubmitResult
	err := s.txManager.InTransaction(ctx, func(uow ports.UnitOfWork) error {
		t, err := wager.NewExternal(params)
		if err != nil {
			return err
		}

		// Violação de unicidade é reentrega do provedor: a identidade já
		// foi processada, então a resposta é o registro anterior — com a
		// conferência de que o payload traz o mesmo conteúdo.
		if err := uow.Transactions().Insert(ctx, t); err != nil {
			var dup *wager.Duplicate
			if !errors.As(err, &dup) || dup.Existing == nil {
				return err
			}
			if err := dup.Existing.CheckIdempotencyReplay(params.PayloadHash); err != nil {
				return err
			}
			resultado = &SubmitResult{Transaction: dup.Existing, Replayed: true}
			return nil
		}

		if err := s.effectuaMovimento(ctx, uow, t); err != nil {
			return err
		}

		resultado = &SubmitResult{Transaction: t, Replayed: false}
		return nil
	})
	return resultado, err
}

// effectuaMovimento aplica o efeito da operação no saldo e encerra a
// transação, gravando lancamento, estado e evento na mesma unidade.
func (s *service) effectuaMovimento(ctx context.Context, uow ports.UnitOfWork, t *wager.Transaction) error {
	direction, moves, err := t.Direction()
	if err != nil {
		return err
	}
	if !moves {
		// LOSS não movimenta saldo: só encerra a operação.
		if err := t.MarkProcessed(time.Now()); err != nil {
			return err
		}
		return uow.Transactions().UpdateState(ctx, t)
	}

	wlt, err := uow.Wallets().FindByID(ctx, t.WalletID())
	if err != nil {
		return err
	}

	before := wlt.Balance()
	var after money.Money
	if direction == ledger.Debit {
		if err := wlt.Debit(t.Amount(), time.Now()); err != nil {
			return err
		}
	} else {
		if err := wlt.Credit(t.Amount(), time.Now()); err != nil {
			return err
		}
	}
	after = wlt.Balance()

	if err := uow.Wallets().UpdateBalance(ctx, wlt); err != nil {
		return err
	}

	entry, err := ledger.New(ledger.Params{
		ID:            identifier.New(),
		WalletID:      wlt.ID(),
		TransactionID: t.ID(),
		Direction:     direction,
		Amount:        t.Amount(),
		BalanceBefore: before,
		BalanceAfter:  after,
		Now:           time.Now(),
	})
	if err != nil {
		return err
	}
	if err := uow.Ledger().Append(ctx, entry); err != nil {
		return err
	}

	if err := t.MarkProcessed(time.Now()); err != nil {
		return err
	}
	if err := uow.Transactions().UpdateState(ctx, t); err != nil {
		return err
	}

	evt, err := event.NewEvent(
		identifier.New(),
		"wallet",
		wlt.ID(),
		"WalletBalanceChanged",
		1,
		[]byte(`{"walletId":"`+wlt.ID()+`","balance":"`+after.String()+`"}`),
		time.Now(),
	)
	if err != nil {
		return err
	}
	return uow.Events().Append(ctx, evt)
}

// GetTransaction devolve a transação pela identidade interna.
func (s *service) GetTransaction(ctx context.Context, id string) (*wager.Transaction, error) {
	return s.wagerRepo.FindByID(ctx, id)
}

// GetTransactionByExternal devolve a transação pelo par do provedor.
func (s *service) GetTransactionByExternal(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	return s.wagerRepo.FindByExternalID(ctx, providerID, externalID)
}
