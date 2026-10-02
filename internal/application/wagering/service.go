// Package wagering contém os casos de uso de apostas.
//
// A camada de aplicação conhece apenas ports: o agregado do domínio e a
// transação provida pela infraestrutura. Todo efeito (saldo, lançamento,
// estado, evento) acontece dentro da mesma unidade de trabalho — nenhuma
// resposta de negócio é dada sem estar persistida.
package wagering

import (
	"context"
	"errors"
	"time"

	"github.com/shimigui/go-challenge/internal/application/events"
	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	domainwallet "github.com/shimigui/go-challenge/internal/domain/wallet"
)

// Códigos de rejeição estáveis, devolvidos ao provedor e publicados nos
// eventos. São contrato: o consumidor compara por eles.
const (
	// failureInsufficientFunds é o débito recusado por falta de saldo.
	failureInsufficientFunds = "INSUFFICIENT_FUNDS"
	// failureInsufficientReversal é a reversão (ROLLBACK) recusada porque o
	// saldo atual não comporta desfazer o crédito original.
	failureInsufficientReversal = "INSUFFICIENT_FUNDS_REVERSAL"
	// failureReferenceMismatch é a referência localizada mas incompatível
	// (provedor, jogador, moeda ou valor divergentes).
	failureReferenceMismatch = "REFERENCE_MISMATCH"
	// failureReferenceNotFound é a referência que nunca chegou dentro do
	// prazo de tentativas do worker.
	failureReferenceNotFound = "REFERENCE_NOT_FOUND"
)

// Parâmetros do worker de referências (§6.3/§7): espera base do backoff
// exponencial, limite de tentativas antes da rejeição definitiva e tamanho
// do lote de retomada.
const (
	referenceBaseWait     = 2 * time.Second
	maxReferenceAttempts  = 5
	limitReferenceBatch   = 100
	referencePollInterval = time.Second
)

// SubmitResult devolve a transação respondida e se ela foi reentrega.
//
// Replayed=true significa que o provedor repetiu uma identidade já
// processada: a resposta é o registro anterior, sem reprocessar. Toda
// resposta de negócio — PROCESSED, PENDING_REFERENCE ou REJECTED — vem
// como resultado, nunca como erro.
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

		if err := processa(ctx, uow, t, params.Now); err != nil {
			return err
		}

		resultado = &SubmitResult{Transaction: t, Replayed: false}
		return nil
	})
	return resultado, err
}

// processa conduz a operação recém-inserida ao seu destino.
//
// Reversões resolvem a referência primeiro: processam na hora quando ela já
// existe, esperam quando ainda não chegou e rejeitam quando a referência é
// encontrada mas contradiz a operação. Movimentos simples (BET, WIN, LOSS)
// vão direto ao efeito. Falha de negócio aqui nunca sobe como erro — vira
// transação REJECTED persistida.
func processa(ctx context.Context, uow ports.UnitOfWork, t *wager.Transaction, agora time.Time) error {
	if !t.Kind().RequiresReference() {
		return movimenta(ctx, uow, t, "", false, agora)
	}

	ref, err := uow.Transactions().FindByExternalID(ctx, t.ProviderID(), t.ReferenceExternalID())
	if err != nil {
		if errors.Is(err, wager.ErrTransactionNotFound) {
			return esperaReferencia(ctx, uow, t, agora)
		}
		return err
	}
	if ref.State() != wager.StateProcessed {
		// A referência existe, mas ainda não terminou: aguarda, não rejeita.
		return esperaReferencia(ctx, uow, t, agora)
	}
	if err := t.ValidateReference(ref); err != nil {
		if errors.Is(err, wager.ErrReferenceNotProcessed) {
			return esperaReferencia(ctx, uow, t, agora)
		}
		return rejeita(ctx, uow, t, failureReferenceMismatch, err.Error(), agora)
	}

	// Referência válida: persiste a resolução interna e segue para o
	// movimento. A guarda SQL aceita PENDING (submissão com referência já
	// disponível) e PENDING_REFERENCE (retomada do worker).
	if err := t.ResolveReference(ref.ID(), agora); err != nil {
		return err
	}
	if err := uow.Transactions().ResolveReference(ctx, t); err != nil {
		return err
	}

	direction, err := t.ReversalDirection(ref)
	if err != nil {
		return err
	}
	return movimenta(ctx, uow, t, direction, true, agora)
}

// esperaReferencia persiste a espera pela referência ou a rejeição
// definitiva quando as tentativas se esgotam.
//
// Na primeira espera (submissão) a operação entra em PENDING_REFERENCE com
// o prazo base e o evento é publicado. Nas retomadas (worker) cada tentativa
// conta com backoff exponencial; esgotado o limite, REJECTED.
func esperaReferencia(ctx context.Context, uow ports.UnitOfWork, t *wager.Transaction, agora time.Time) error {
	if t.State() == wager.StatePendingReference {
		esgotado, _, err := t.RegisterReferenceAttempt(referenceBaseWait, maxReferenceAttempts, agora)
		if err != nil {
			return err
		}
		if esgotado {
			return rejeita(ctx, uow, t, failureReferenceNotFound,
				"referencia "+t.ReferenceExternalID()+" nao chegou no prazo", agora)
		}
		return uow.Transactions().UpdateState(ctx, t)
	}

	if err := t.AwaitReference(agora.Add(referenceBaseWait), agora); err != nil {
		return err
	}
	if err := uow.Transactions().UpdateState(ctx, t); err != nil {
		return err
	}
	payload, err := events.WagerTransactionPendingReference(t)
	if err != nil {
		return err
	}
	evt, err := events.NewEvent(
		"wager_transaction", t.ID(),
		events.TypeWagerTransactionPendingReference, payload, agora,
	)
	if err != nil {
		return err
	}
	return uow.Events().Append(ctx, evt)
}

// rejeita encerra a operação como REJECTED, persistindo a recusa e
// publicando o evento na mesma unidade de trabalho.
func rejeita(ctx context.Context, uow ports.UnitOfWork, t *wager.Transaction, code, message string, agora time.Time) error {
	if err := t.MarkRejected(code, message, agora); err != nil {
		return err
	}
	if err := uow.Transactions().UpdateState(ctx, t); err != nil {
		return err
	}
	payload, err := events.WagerTransactionRejected(t)
	if err != nil {
		return err
	}
	evt, err := events.NewEvent(
		"wager_transaction", t.ID(),
		events.TypeWagerTransactionRejected, payload, agora,
	)
	if err != nil {
		return err
	}
	return uow.Events().Append(ctx, evt)
}

// movimenta aplica o efeito da operação no saldo e encerra a transação,
// gravando lançamento, estado e eventos na mesma unidade.
//
// LOSS não movimenta: conclui registrando o saldo observado. Débito que não
// cabe no saldo é rejeição persistida (o valor do código depende de a
// operação ser uma reversão). Todo PROCESSED carrega o saldo observado,
// que o replay devolve.
func movimenta(ctx context.Context, uow ports.UnitOfWork, t *wager.Transaction, direction ledger.Direction, reversao bool, agora time.Time) error {
	wlt, err := uow.Wallets().FindByID(ctx, t.WalletID())
	if err != nil {
		return err
	}

	if !t.MovesMoney() {
		// LOSS: sem débito nem crédito, mas com saldo observado — o valor
		// de replay do provedor.
		if err := t.SetObservedBalance(wlt.Balance()); err != nil {
			return err
		}
		if err := t.MarkProcessed(agora); err != nil {
			return err
		}
		if err := uow.Transactions().UpdateState(ctx, t); err != nil {
			return err
		}
		return publicaProcessado(ctx, uow, t, agora)
	}

	// Movimento simples deriva o sentido do próprio tipo; reversão já veio
	// com o sentido resolvido pela referência.
	if !reversao {
		var err2 error
		direction, _, err2 = t.Direction()
		if err2 != nil {
			return err2
		}
	}

	before := wlt.Balance()
	if direction == ledger.Debit {
		err = wlt.Debit(t.Amount(), agora)
	} else {
		err = wlt.Credit(t.Amount(), agora)
	}
	if err != nil {
		if errors.Is(err, domainwallet.ErrInsufficientFunds) {
			code := failureInsufficientFunds
			if reversao {
				code = failureInsufficientReversal
			}
			return rejeita(ctx, uow, t, code, err.Error(), agora)
		}
		return err
	}
	after := wlt.Balance()

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
		Now:           agora,
	})
	if err != nil {
		return err
	}
	if err := uow.Ledger().Append(ctx, entry); err != nil {
		return err
	}

	if err := t.SetObservedBalance(after); err != nil {
		return err
	}
	if err := t.MarkProcessed(agora); err != nil {
		return err
	}
	if err := uow.Transactions().UpdateState(ctx, t); err != nil {
		return err
	}

	if err := publicaProcessado(ctx, uow, t, agora); err != nil {
		return err
	}
	return publicaSaldo(ctx, uow, t, direction, before, after, wlt.Version(), agora)
}

// publicaProcessado anuncia a conclusão da operação no agregado da
// transação.
func publicaProcessado(ctx context.Context, uow ports.UnitOfWork, t *wager.Transaction, agora time.Time) error {
	payload, err := events.WagerTransactionProcessed(t)
	if err != nil {
		return err
	}
	evt, err := events.NewEvent(
		"wager_transaction", t.ID(),
		events.TypeWagerTransactionProcessed, payload, agora,
	)
	if err != nil {
		return err
	}
	return uow.Events().Append(ctx, evt)
}

// publicaSaldo anuncia a mudança de saldo no agregado da carteira.
func publicaSaldo(ctx context.Context, uow ports.UnitOfWork, t *wager.Transaction, direction ledger.Direction, before, after money.Money, walletVersion int64, agora time.Time) error {
	payload, err := events.WalletBalanceChanged(
		t.WalletID(), t.ID(), direction, t.Amount(), before, after, walletVersion,
	)
	if err != nil {
		return err
	}
	evt, err := events.NewEvent(
		"wallet", t.WalletID(),
		events.TypeWalletBalanceChanged, payload, agora,
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
