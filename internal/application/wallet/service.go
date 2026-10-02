// Package wallet contém os casos de uso da carteira.
//
// A camada de aplicação conhece apenas ports: os agregados do domínio,
// os repositórios de leitura e a transação provida pela infraestrutura.
package wallet

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

// NowFunc devolve o horário do relógio da aplicação.
type NowFunc func() time.Time

// Service define as operações de carteira que a interface HTTP usa.
type Service interface {
	CreateWallet(ctx context.Context, playerID string, opening money.Money, now NowFunc) (*domainwallet.Wallet, error)
	GetWallet(ctx context.Context, id string) (*domainwallet.Wallet, error)
	GetLedger(ctx context.Context, walletID string, cursor *string, limit int) ([]*ledger.Entry, *string, error)
	Reconcile(ctx context.Context, walletID string) (*Reconciliation, error)
}

// Reconciliation guarda o resultado da conciliação da carteira.
type Reconciliation struct {
	WalletID          string
	StoredBalance     money.Money
	CalculatedBalance money.Money
	Difference        money.Money
	Consistent        bool
	CheckedEntries    int
}

// service implementa os casos de uso de carteira.
type service struct {
	walletRepo domainwallet.WalletRepository
	ledgerRepo ledger.LedgerRepository
	txManager  ports.TransactionManager
}

// NewService monta o caso de uso com as portas de saída.
func NewService(
	walletRepo domainwallet.WalletRepository,
	ledgerRepo ledger.LedgerRepository,
	txManager ports.TransactionManager,
) Service {
	return &service{
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
		txManager:  txManager,
	}
}

// CreateWallet abre a carteira do jogador na moeda do saldo inicial.
//
// Abertura com saldo positivo lança o OPENING interno, o crédito no ledger
// e os eventos WagerTransactionProcessed e WalletBalanceChanged no mesmo
// commit: a carteira, o lançamento e os fatos nascem juntos ou não nascem.
// Abertura com saldo zero não movimenta nada — só a carteira existe.
func (s *service) CreateWallet(ctx context.Context, playerID string, opening money.Money, now NowFunc) (*domainwallet.Wallet, error) {
	var wlt *domainwallet.Wallet
	err := s.txManager.InTransaction(ctx, func(uow ports.UnitOfWork) error {
		ts := now()
		w, err := domainwallet.New(domainwallet.Params{
			ID:       identifier.New(),
			PlayerID: playerID,
			Opening:  opening,
			Now:      ts,
		})
		if err != nil {
			return err
		}
		if err := uow.Wallets().Insert(ctx, w); err != nil {
			return err
		}
		if opening.IsPositive() {
			if err := s.abreComMovimento(ctx, uow, w, opening, ts); err != nil {
				return err
			}
		}
		wlt = w
		return nil
	})
	return wlt, err
}

// abreComMovimento lança a abertura com saldo: OPENING, crédito no ledger e
// os eventos do fato, tudo na mesma unidade de trabalho.
func (s *service) abreComMovimento(ctx context.Context, uow ports.UnitOfWork, w *domainwallet.Wallet, opening money.Money, ts time.Time) error {
	op, err := wager.NewOpening(wager.OpeningParams{
		ID:       identifier.New(),
		PlayerID: w.PlayerID(),
		WalletID: w.ID(),
		Amount:   opening,
		Now:      ts,
	})
	if err != nil {
		return err
	}
	if err := uow.Transactions().Insert(ctx, op); err != nil {
		// A carteira já é única por jogador+moeda; um OPENING repetido só
		// chegaria aqui por corrida, e o conflito de negócio é o mesmo.
		var dup *wager.Duplicate
		if errors.As(err, &dup) && dup.Existing != nil {
			return domainwallet.ErrDuplicateWallet
		}
		return err
	}

	zero := money.Zero(w.Currency())
	if err := op.SetObservedBalance(opening); err != nil {
		return err
	}
	if err := op.MarkProcessed(ts); err != nil {
		return err
	}
	if err := uow.Transactions().UpdateState(ctx, op); err != nil {
		return err
	}

	entry, err := ledger.ForCredit(
		identifier.New(), w.ID(), op.ID(), opening, zero, opening, ts,
	)
	if err != nil {
		return err
	}
	if err := uow.Ledger().Append(ctx, entry); err != nil {
		return err
	}

	processed, err := events.WagerTransactionProcessed(op)
	if err != nil {
		return err
	}
	evtProcessed, err := events.NewEvent(
		"wager_transaction", op.ID(),
		events.TypeWagerTransactionProcessed, processed, ts,
	)
	if err != nil {
		return err
	}
	if err := uow.Events().Append(ctx, evtProcessed); err != nil {
		return err
	}

	changed, err := events.WalletBalanceChanged(
		w.ID(), op.ID(), ledger.Credit, opening, zero, opening, w.Version(),
	)
	if err != nil {
		return err
	}
	evtChanged, err := events.NewEvent(
		"wallet", w.ID(),
		events.TypeWalletBalanceChanged, changed, ts,
	)
	if err != nil {
		return err
	}
	return uow.Events().Append(ctx, evtChanged)
}

// GetWallet devolve a carteira pela identidade.
func (s *service) GetWallet(ctx context.Context, id string) (*domainwallet.Wallet, error) {
	return s.walletRepo.FindByID(ctx, id)
}

// GetLedger devolve o extrato da carteira, do mais novo para o mais antigo.
func (s *service) GetLedger(ctx context.Context, walletID string, cursor *string, limit int) ([]*ledger.Entry, *string, error) {
	entries, err := s.ledgerRepo.ListByWallet(ctx, walletID, limit)
	if err != nil {
		return nil, nil, err
	}
	return entries, cursor, nil
}

// Reconcile reconstrói o saldo pelo ledger e compara com o armazenado.
func (s *service) Reconcile(ctx context.Context, walletID string) (*Reconciliation, error) {
	wlt, err := s.walletRepo.FindByID(ctx, walletID)
	if err != nil {
		return nil, err
	}

	entries, err := s.ledgerRepo.ListByWalletAll(ctx, walletID)
	if err != nil {
		return nil, err
	}

	calculated := money.Zero(wlt.Currency())
	if len(entries) > 0 {
		calculated, err = sumEntries(entries)
		if err != nil {
			return nil, err
		}
	}

	diff, err := wlt.Balance().Sub(calculated)
	if err != nil {
		return nil, err
	}

	return &Reconciliation{
		WalletID:          walletID,
		StoredBalance:     wlt.Balance(),
		CalculatedBalance: calculated,
		Difference:        diff,
		Consistent:        diff.IsZero(),
		CheckedEntries:    len(entries),
	}, nil
}

// sumEntries soma os lançamentos com sinal, reconstruindo o saldo do
// ledger para a reconciliação.
//
// A soma começa em zero na moeda do primeiro lançamento: somar valores
// sempre preserva a moeda da carteira, já que o ledger é todo na mesma moeda.
func sumEntries(entries []*ledger.Entry) (money.Money, error) {
	if len(entries) == 0 {
		return money.Money{}, nil
	}
	total := money.Zero(entries[0].Amount().Currency())
	for _, e := range entries {
		signed, err := e.SignedAmount()
		if err != nil {
			return money.Money{}, err
		}
		total, err = total.Add(signed)
		if err != nil {
			return money.Money{}, err
		}
	}
	return total, nil
}
