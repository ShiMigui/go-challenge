// Package wallet contém os casos de uso da carteira.
//
// A camada de aplicação conhece apenas ports: os agregados do domínio,
// os repositórios de leitura e a transação provida pela infraestrutura.
package wallet

import (
	"context"
	"time"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
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
func (s *service) CreateWallet(ctx context.Context, playerID string, opening money.Money, now NowFunc) (*domainwallet.Wallet, error) {
	var wlt *domainwallet.Wallet
	err := s.txManager.InTransaction(ctx, func(uow ports.UnitOfWork) error {
		w, err := domainwallet.New(domainwallet.Params{
			ID:       identifier.New(),
			PlayerID: playerID,
			Opening:  opening,
			Now:      now(),
		})
		if err != nil {
			return err
		}
		if err := uow.Wallets().Insert(ctx, w); err != nil {
			return err
		}
		wlt = w
		return nil
	})
	return wlt, err
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

	calculated, err := sumEntries(entries)
	if err != nil {
		return nil, err
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
func sumEntries(entries []*ledger.Entry) (money.Money, error) {
	var total money.Money
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
