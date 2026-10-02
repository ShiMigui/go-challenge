package service

import (
	"context"
	"database/sql"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
	"github.com/shimigui/go-challenge/internal/repository"
)

type WalletService struct {
	walletRepo wallet.WalletRepository
	ledgerRepo ledger.LedgerRepository
	txManager  repository.TransactionManager
}

func NewWalletService(
	walletRepo wallet.WalletRepository,
	ledgerRepo ledger.LedgerRepository,
	txManager repository.TransactionManager,
) *WalletService {
	return &WalletService{
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
		txManager:  txManager,
	}
}

func (s *WalletService) CreateWallet(ctx context.Context, playerID string, opening money.Money, now wallet.NowFunc) (*wallet.Wallet, error) {
	var wlt *wallet.Wallet
	err := s.txManager.InTransaction(ctx, func(txn *sql.Tx) error {
		walletRepo := repository.NewWalletRepository(txn)
		w, err := wallet.New(wallet.Params{
			ID:       identifier.New(),
			PlayerID: playerID,
			Opening:  opening,
			Now:      now(),
		})
		if err != nil {
			return err
		}
		if err := walletRepo.Insert(ctx, w); err != nil {
			return err
		}
		wlt = w
		return nil
	})
	return wlt, err
}

func (s *WalletService) GetWallet(ctx context.Context, id string) (*wallet.Wallet, error) {
	return s.walletRepo.FindByID(ctx, id)
}

func (s *WalletService) GetLedger(ctx context.Context, walletID string, cursor *string, limit int) ([]*ledger.Entry, *string, error) {
	entries, err := s.ledgerRepo.ListByWallet(ctx, walletID, limit)
	if err != nil {
		return nil, nil, err
	}
	return entries, cursor, nil
}

func (s *WalletService) Reconcile(ctx context.Context, walletID string) (*wallet.Reconciliation, error) {
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

	return &wallet.Reconciliation{
		WalletID:          walletID,
		StoredBalance:     wlt.Balance(),
		CalculatedBalance: calculated,
		Difference:        diff,
		Consistent:        diff.IsZero(),
		CheckedEntries:    len(entries),
	}, nil
}
