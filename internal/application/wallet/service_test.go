package wallet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/application/wallet"
	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	domainwallet "github.com/shimigui/go-challenge/internal/domain/wallet"
)

// ===== fakes em memória =====
//
// Os repositórios guardam cópias (valores), não ponteiros, para que o
// rollback da transação fake restaure o estado com fidelidade: mutações no
// agregado devolvido por FindByID nunca vazam para o "banco".

type memWalletRepo struct {
	porID map[string]domainwallet.Wallet

	// Gatilhos de falha configuráveis por teste.
	errInsert   error
	errFind     error
	errUpdate   error
	findMissing bool
}

func newMemWalletRepo() *memWalletRepo {
	return &memWalletRepo{porID: map[string]domainwallet.Wallet{}}
}

func (r *memWalletRepo) Insert(ctx context.Context, w *domainwallet.Wallet) error {
	if r.errInsert != nil {
		return r.errInsert
	}
	if _, ok := r.porID[w.ID()]; ok {
		return domainwallet.ErrDuplicateWallet
	}
	r.porID[w.ID()] = *w
	return nil
}

func (r *memWalletRepo) UpdateBalance(ctx context.Context, w *domainwallet.Wallet) error {
	if r.errUpdate != nil {
		return r.errUpdate
	}
	r.porID[w.ID()] = *w
	return nil
}

func (r *memWalletRepo) FindByID(ctx context.Context, id string) (*domainwallet.Wallet, error) {
	if r.findMissing {
		return nil, r.errFind
	}
	w, ok := r.porID[id]
	if !ok {
		return nil, domainwallet.ErrWalletNotFound
	}
	copia := w
	return &copia, nil
}

func (r *memWalletRepo) FindByPlayerAndCurrency(ctx context.Context, playerID string, currency money.Currency) (*domainwallet.Wallet, error) {
	for _, w := range r.porID {
		if w.PlayerID() == playerID && w.Currency() == currency {
			copia := w
			return &copia, nil
		}
	}
	return nil, domainwallet.ErrWalletNotFound
}

func (r *memWalletRepo) LockByID(ctx context.Context, id string) (*domainwallet.Wallet, error) {
	return r.FindByID(ctx, id)
}

type memLedgerRepo struct {
	lancamentos []ledger.Entry
	errAppend   error
	errList     error
}

func (r *memLedgerRepo) Append(ctx context.Context, e *ledger.Entry) error {
	if r.errAppend != nil {
		return r.errAppend
	}
	r.lancamentos = append(r.lancamentos, *e)
	return nil
}

func (r *memLedgerRepo) FindByTransaction(ctx context.Context, transactionID string) (*ledger.Entry, error) {
	for _, e := range r.lancamentos {
		if e.TransactionID() == transactionID {
			copia := e
			return &copia, nil
		}
	}
	return nil, errors.New("lancamento nao encontrado")
}

func (r *memLedgerRepo) ListByWallet(ctx context.Context, walletID string, limite int) ([]*ledger.Entry, error) {
	if r.errList != nil {
		return nil, r.errList
	}
	out := make([]*ledger.Entry, 0, len(r.lancamentos))
	for i := range r.lancamentos {
		e := r.lancamentos[i]
		if e.WalletID() == walletID {
			copia := e
			out = append(out, &copia)
		}
	}
	return out, nil
}

func (r *memLedgerRepo) ListByWalletAll(ctx context.Context, walletID string) ([]*ledger.Entry, error) {
	return r.ListByWallet(ctx, walletID, 0)
}

// memTxManager roda o callback com uma UnitOfWork que compartilha os mesmos
// stores e desfaz tudo se a função falhar — o comportamento da transação SQL.
type memTxManager struct {
	uow  *memUoW
	rods int
}

func newMemTxManager() *memTxManager {
	return &memTxManager{uow: newMemUoW()}
}

func (m *memTxManager) InTransaction(ctx context.Context, fn func(ports.UnitOfWork) error) error {
	m.rods++
	carteiras, lancamentos := m.uow.snapshot()
	if err := fn(m.uow); err != nil {
		m.uow.restore(carteiras, lancamentos)
		return err
	}
	return nil
}

type memUoW struct {
	wallets  *memWalletRepo
	ledger   *memLedgerRepo
	txns     *memTxnRepo
	events   *memEventsRepo
	messages *memInboxRepo
}

func newMemUoW() *memUoW {
	return &memUoW{
		wallets:  newMemWalletRepo(),
		ledger:   &memLedgerRepo{},
		txns:     &memTxnRepo{},
		events:   &memEventsRepo{},
		messages: &memInboxRepo{},
	}
}

func (u *memUoW) Wallets() domainwallet.WalletRepository { return u.wallets }
func (u *memUoW) Ledger() ledger.LedgerRepository        { return u.ledger }
func (u *memUoW) Transactions() wager.WagerTransactionRepository {
	return u.txns
}
func (u *memUoW) Events() event.OutboxRepository  { return u.events }
func (u *memUoW) Messages() ports.InboxRepository { return u.messages }

func (u *memUoW) snapshot() (wallets map[string]domainwallet.Wallet, lancamentos []ledger.Entry) {
	wallets = make(map[string]domainwallet.Wallet, len(u.wallets.porID))
	for k, v := range u.wallets.porID {
		wallets[k] = v
	}
	lancamentos = make([]ledger.Entry, len(u.ledger.lancamentos))
	copy(lancamentos, u.ledger.lancamentos)
	return wallets, lancamentos
}

func (u *memUoW) restore(carteiras map[string]domainwallet.Wallet, lancamentos []ledger.Entry) {
	u.wallets.porID = carteiras
	u.ledger.lancamentos = lancamentos
}

// memTxnRepo, memEventsRepo e memInboxRepo só satisfazem a UnitOfWork no
// caso de uso de carteira — ele não escreve nesses stores.
type memTxnRepo struct{}

func (r *memTxnRepo) Insert(ctx context.Context, tx *wager.Transaction) error { return nil }
func (r *memTxnRepo) FindByID(ctx context.Context, id string) (*wager.Transaction, error) {
	return nil, wager.ErrTransactionNotFound
}
func (r *memTxnRepo) FindByExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	return nil, wager.ErrTransactionNotFound
}
func (r *memTxnRepo) FindByIdempotencyKey(ctx context.Context, providerID, key string) (*wager.Transaction, error) {
	return nil, wager.ErrTransactionNotFound
}
func (r *memTxnRepo) UpdateState(ctx context.Context, tx *wager.Transaction) error { return nil }
func (r *memTxnRepo) ResolveReference(ctx context.Context, tx *wager.Transaction) error {
	return nil
}
func (r *memTxnRepo) ListPendingReferences(ctx context.Context, agora time.Time, limite int) ([]*wager.Transaction, error) {
	return nil, nil
}

type memEventsRepo struct{}

func (r *memEventsRepo) Append(ctx context.Context, e *event.Event) error { return nil }
func (r *memEventsRepo) ClaimBatch(ctx context.Context, worker string, limite int, now time.Time) ([]*event.Event, error) {
	return nil, nil
}
func (r *memEventsRepo) MarkPublished(ctx context.Context, id string, now time.Time) error {
	return nil
}
func (r *memEventsRepo) Reschedule(ctx context.Context, id string, proximaTentativa, now time.Time) error {
	return nil
}

type memInboxRepo struct{}

func (r *memInboxRepo) TryBegin(ctx context.Context, consumer, messageID, messageHash string, now time.Time) (bool, error) {
	return true, nil
}
func (r *memInboxRepo) Complete(ctx context.Context, consumer, messageID string, now time.Time) error {
	return nil
}
func (r *memInboxRepo) IsCompleted(ctx context.Context, consumer, messageID string) (bool, error) {
	return false, nil
}
func (r *memInboxRepo) CountAttempts(ctx context.Context, consumer, messageID string) (int, error) {
	return 0, nil
}

// ===== helpers para montar valores prontos =====

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// errBancoForaDoAr é a sentinela de falha do port de escrita nos testes.
var errBancoForaDoAr = errors.New("banco fora do ar")

func novaCarteiraPronta(t *testing.T, id, player, saldo string) *domainwallet.Wallet {
	t.Helper()
	w, err := domainwallet.New(domainwallet.Params{
		ID:       id,
		PlayerID: player,
		Opening:  money.MustParse(saldo, money.BRL),
		Now:      t0,
	})
	if err != nil {
		t.Fatalf("carteira pronta: %v", err)
	}
	return w
}

func novoLancamento(t *testing.T, id, walletID, txnID string, direction ledger.Direction, valor, antes, depois string) *ledger.Entry {
	t.Helper()
	e, err := ledger.New(ledger.Params{
		ID:            id,
		WalletID:      walletID,
		TransactionID: txnID,
		Direction:     direction,
		Amount:        money.MustParse(valor, money.BRL),
		BalanceBefore: money.MustParse(antes, money.BRL),
		BalanceAfter:  money.MustParse(depois, money.BRL),
		Now:           t0,
	})
	if err != nil {
		t.Fatalf("lancamento: %v", err)
	}
	return e
}

// ===== CreateWallet =====

func TestCreateWalletComSaldo(t *testing.T) {
	tm := newMemTxManager()
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	wlt, err := svc.CreateWallet(context.Background(), "player-1",
		money.MustParse("100.00", money.BRL), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("CreateWallet: %v", err)
	}

	if wlt == nil || wlt.ID() == "" {
		t.Fatal("carteira sem id")
	}
	if wlt.PlayerID() != "player-1" {
		t.Errorf("player = %q", wlt.PlayerID())
	}
	if got := wlt.Balance().String(); got != "100.00" {
		t.Errorf("saldo = %s, quer 100.00", got)
	}
	if wlt.Version() != 1 {
		t.Errorf("versão inicial = %d, quer 1", wlt.Version())
	}
	if wlt.Currency() != money.BRL {
		t.Errorf("moeda = %s", wlt.Currency())
	}

	// A carteira foi gravada pelo port de escrita dentro da transação.
	if tm.rods != 1 {
		t.Errorf("InTransaction chamado %d vezes, quer 1", tm.rods)
	}
	persistida, err := tm.uow.wallets.FindByID(context.Background(), wlt.ID())
	if err != nil {
		t.Fatalf("carteira não persistida: %v", err)
	}
	if persistida.Balance().String() != "100.00" {
		t.Errorf("saldo persistido = %s", persistida.Balance())
	}
}

func TestCreateWalletComSaldoZero(t *testing.T) {
	// A spec aceita zero no saldo inicial.
	tm := newMemTxManager()
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	wlt, err := svc.CreateWallet(context.Background(), "player-1",
		money.Zero(money.BRL), func() time.Time { return t0 })
	if err != nil {
		t.Fatalf("CreateWallet com zero: %v", err)
	}
	if !wlt.Balance().IsZero() {
		t.Errorf("saldo = %s, quer 0.00", wlt.Balance())
	}
	if wlt.Version() != 1 {
		t.Errorf("versão = %d, quer 1", wlt.Version())
	}
}

func TestCreateWalletRejeitaAberturaNegativa(t *testing.T) {
	tm := newMemTxManager()
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	negativa := money.MustNew(-100, money.BRL)
	_, err := svc.CreateWallet(context.Background(), "player-1", negativa, func() time.Time { return t0 })
	if !errors.Is(err, money.ErrNegativeAmount) {
		t.Fatalf("abertura negativa: esperava ErrNegativeAmount, veio %v", err)
	}
	if len(tm.uow.wallets.porID) != 0 {
		t.Error("carteira negativa foi persistida (rollback não desfez)")
	}
}

func TestCreateWalletRejeitaJogadorVazio(t *testing.T) {
	tm := newMemTxManager()
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	_, err := svc.CreateWallet(context.Background(), "",
		money.Zero(money.BRL), func() time.Time { return t0 })
	if !errors.Is(err, identifier.ErrInvalidPlayerID) {
		t.Fatalf("jogador vazio: esperava ErrInvalidPlayerID, veio %v", err)
	}
	if len(tm.uow.wallets.porID) != 0 {
		t.Error("carteira sem jogador foi persistida (rollback não desfez)")
	}
}

func TestCreateWalletPropagaErroDoPortDeEscrita(t *testing.T) {
	tm := newMemTxManager()
	tm.uow.wallets.errInsert = errBancoForaDoAr
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	_, err := svc.CreateWallet(context.Background(), "player-1",
		money.Zero(money.BRL), func() time.Time { return t0 })
	if !errors.Is(err, errBancoForaDoAr) {
		t.Fatalf("esperava erro do port de escrita, veio %v", err)
	}
}

// ===== GetWallet =====

func TestGetWalletOK(t *testing.T) {
	tm := newMemTxManager()
	if err := tm.uow.wallets.Insert(context.Background(),
		novaCarteiraPronta(t, "w-1", "player-1", "75.00")); err != nil {
		t.Fatal(err)
	}
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	got, err := svc.GetWallet(context.Background(), "w-1")
	if err != nil {
		t.Fatalf("GetWallet: %v", err)
	}
	if got.Balance().String() != "75.00" {
		t.Errorf("saldo = %s, quer 75.00", got.Balance())
	}
	if got.Version() != 1 {
		t.Errorf("versão = %d, quer 1", got.Version())
	}
}

func TestGetWalletNaoEncontrado(t *testing.T) {
	tm := newMemTxManager()
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	_, err := svc.GetWallet(context.Background(), "w-inexistente")
	if !errors.Is(err, domainwallet.ErrWalletNotFound) {
		t.Fatalf("esperava ErrWalletNotFound, veio %v", err)
	}
}

func TestGetWalletPropagaFalhaDoRepositorio(t *testing.T) {
	tm := newMemTxManager()
	tm.uow.wallets.errFind = errBancoForaDoAr
	tm.uow.wallets.findMissing = true
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	if _, err := svc.GetWallet(context.Background(), "w-1"); !errors.Is(err, errBancoForaDoAr) {
		t.Fatalf("esperava falha do repositório, veio %v", err)
	}
}

// ===== GetLedger =====

func TestGetLedgerDevolveExtratoECursor(t *testing.T) {
	tm := newMemTxManager()
	tm.uow.ledger.lancamentos = []ledger.Entry{
		*novoLancamento(t, "e-2", "w-1", "t-2", ledger.Debit, "25.00", "100.00", "75.00"),
		*novoLancamento(t, "e-1", "w-1", "t-1", ledger.Credit, "100.00", "0.00", "100.00"),
	}
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	cursor := "eyJkZXNkIjoiZGVzYyJ9"
	entries, next, err := svc.GetLedger(context.Background(), "w-1", &cursor, 50)
	if err != nil {
		t.Fatalf("GetLedger: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entradas = %d, quer 2", len(entries))
	}
	if next == nil || *next != cursor {
		t.Errorf("cursor não repassado: %v", next)
	}
	if entries[0].TransactionID() != "t-2" {
		t.Error("esperava o lançamento mais novo primeiro")
	}
	if got := entries[0].BalanceAfter().String(); got != "75.00" {
		t.Errorf("saldo depois = %s, quer 75.00", got)
	}
}

func TestGetLedgerVazio(t *testing.T) {
	tm := newMemTxManager()
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	entries, _, err := svc.GetLedger(context.Background(), "w-1", nil, 50)
	if err != nil {
		t.Fatalf("GetLedger vazio: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("extrato vazio deveria vir vazio, veio %d", len(entries))
	}
}

func TestGetLedgerPropagaFalhaDoRepositorio(t *testing.T) {
	tm := newMemTxManager()
	tm.uow.ledger.errList = errBancoForaDoAr
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	if _, _, err := svc.GetLedger(context.Background(), "w-1", nil, 50); !errors.Is(err, errBancoForaDoAr) {
		t.Fatalf("esperava falha do repositório, veio %v", err)
	}
}

// ===== Reconcile =====

func TestReconcileConsistente(t *testing.T) {
	tm := newMemTxManager()
	if err := tm.uow.wallets.Insert(context.Background(),
		novaCarteiraPronta(t, "w-1", "player-1", "75.00")); err != nil {
		t.Fatal(err)
	}
	tm.uow.ledger.lancamentos = []ledger.Entry{
		*novoLancamento(t, "e-2", "w-1", "t-2", ledger.Debit, "25.00", "100.00", "75.00"),
		*novoLancamento(t, "e-1", "w-1", "t-1", ledger.Credit, "100.00", "0.00", "100.00"),
	}
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	rec, err := svc.Reconcile(context.Background(), "w-1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !rec.Consistent {
		t.Error("deveria estar consistente")
	}
	if rec.StoredBalance.String() != "75.00" || rec.CalculatedBalance.String() != "75.00" {
		t.Errorf("saldos: armazenado %s, calculado %s", rec.StoredBalance, rec.CalculatedBalance)
	}
	if rec.Difference.String() != "0.00" {
		t.Errorf("difference = %s, quer 0.00", rec.Difference)
	}
	if rec.CheckedEntries != 2 {
		t.Errorf("entradas conferidas = %d, quer 2", rec.CheckedEntries)
	}
	if rec.WalletID != "w-1" {
		t.Errorf("walletId = %q", rec.WalletID)
	}
}

func TestReconcileDivergente(t *testing.T) {
	tm := newMemTxManager()
	if err := tm.uow.wallets.Insert(context.Background(),
		novaCarteiraPronta(t, "w-1", "player-1", "100.00")); err != nil {
		t.Fatal(err)
	}
	// O ledger registrou apenas 80.00 de créditos: divergência de 20.00.
	tm.uow.ledger.lancamentos = []ledger.Entry{
		*novoLancamento(t, "e-1", "w-1", "t-1", ledger.Credit, "80.00", "0.00", "80.00"),
	}
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	rec, err := svc.Reconcile(context.Background(), "w-1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rec.Consistent {
		t.Error("deveria estar inconsistente")
	}
	if rec.StoredBalance.String() != "100.00" {
		t.Errorf("armazenado = %s, quer 100.00", rec.StoredBalance)
	}
	if rec.CalculatedBalance.String() != "80.00" {
		t.Errorf("calculado = %s, quer 80.00", rec.CalculatedBalance)
	}
	// difference = armazenado − calculado (contrato do §9).
	if rec.Difference.String() != "20.00" {
		t.Errorf("difference = %s, quer 20.00", rec.Difference)
	}
}

func TestReconcileCarteiraInexistente(t *testing.T) {
	tm := newMemTxManager()
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	_, err := svc.Reconcile(context.Background(), "w-zzz")
	if !errors.Is(err, domainwallet.ErrWalletNotFound) {
		t.Fatalf("esperava ErrWalletNotFound, veio %v", err)
	}
}
