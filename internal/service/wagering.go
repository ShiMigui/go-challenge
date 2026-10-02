package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/repository"
)

type WageringService struct {
	wagerRepo  repository.WagerTransactionRepository
	walletRepo repository.WalletRepository
	ledgerRepo repository.LedgerRepository
	outboxRepo repository.OutboxRepository
	txManager  repository.TransactionManager
}

func NewWageringService(
	wagerRepo repository.WagerTransactionRepository,
	walletRepo repository.WalletRepository,
	ledgerRepo repository.LedgerRepository,
	outboxRepo repository.OutboxRepository,
	txManager repository.TransactionManager,
) *WageringService {
	return &WageringService{
		wagerRepo:  wagerRepo,
		walletRepo: walletRepo,
		ledgerRepo: ledgerRepo,
		outboxRepo: outboxRepo,
		txManager:  txManager,
	}
}

func (s *WageringService) SubmitTransaction(ctx context.Context, params wager.ExternalParams) (*wager.Transaction, error) {
	var tx *wager.Transaction
	err := s.txManager.InTransaction(ctx, func(txn *sql.Tx) error {
		wagerRepo := repository.NewWagerTransactionRepository(txn)
		walletRepo := repository.NewWalletRepository(txn)
		ledgerRepo := repository.NewLedgerRepository(txn)
		outboxRepo := repository.NewOutboxRepository(txn)

		t, err := wager.NewExternal(params)
		if err != nil {
			return err
		}
		tx = t

		if err := wagerRepo.CheckIdempotency(ctx, params.IdempotencyKey, params.PayloadHash); err != nil {
			return err
		}

		if err := wagerRepo.Insert(ctx, tx); err != nil {
			return err
		}

		if tx.MovesMoney() {
			wlt, err := walletRepo.FindByID(ctx, tx.WalletID())
			if err != nil {
				return err
			}

			before := wlt.Balance()
			var after money.Money
			var direction ledger.Direction
			if tx.Kind() == wager.KindBet {
				direction = ledger.Debit
				if err := wlt.Debit(tx.Amount(), time.Now()); err != nil {
					return err
				}
				after = wlt.Balance()
			} else if tx.Kind() == wager.KindWin || tx.Kind() == wager.KindRefund || tx.Kind() == wager.KindRollback {
				direction = ledger.Credit
				if err := wlt.Credit(tx.Amount(), time.Now()); err != nil {
					return err
				}
				after = wlt.Balance()
			}

			if err := walletRepo.UpdateBalance(ctx, wlt); err != nil {
				return err
			}

			entry, err := ledger.New(ledger.Params{
				ID:            identifier.New(),
				WalletID:      wlt.ID(),
				TransactionID: tx.ID(),
				Direction:     direction,
				Amount:        tx.Amount(),
				BalanceBefore: before,
				BalanceAfter:  after,
				Now:           time.Now(),
			})
			if err != nil {
				return err
			}
			if err := ledgerRepo.Append(ctx, entry); err != nil {
				return err
			}

			tx.MarkProcessed(time.Now())
			if err := wagerRepo.UpdateState(ctx, tx); err != nil {
				return err
			}

			evt, err := repository.NewEvent(
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
			if err := outboxRepo.Append(ctx, evt); err != nil {
				return err
			}
		} else if tx.Kind() == wager.KindLoss {
			tx.MarkProcessed(time.Now())
			if err := wagerRepo.UpdateState(ctx, tx); err != nil {
				return err
			}
		}

		return nil
	})
	return tx, err
}

func (s *WageringService) GetTransaction(ctx context.Context, id string) (*wager.Transaction, error) {
	return s.wagerRepo.FindByID(ctx, id)
}

func (s *WageringService) GetTransactionByExternal(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	return s.wagerRepo.FindByExternalID(ctx, providerID, externalID)
}
