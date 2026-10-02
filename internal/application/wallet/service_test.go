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
	carteiras, lancamentos, txns, eventos := m.uow.snapshot()
	if err := fn(m.uow); err != nil {
		m.uow.restore(carteiras, lancamentos, txns, eventos)
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
		txns:     newMemTxnRepo(),
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

func (u *memUoW) snapshot() (wallets map[string]domainwallet.Wallet, lancamentos []ledger.Entry, txns map[string]wager.Transaction, eventos []event.Event) {
	wallets = make(map[string]domainwallet.Wallet, len(u.wallets.porID))
	for k, v := range u.wallets.porID {
		wallets[k] = v
	}
	lancamentos = make([]ledger.Entry, len(u.ledger.lancamentos))
	copy(lancamentos, u.ledger.lancamentos)
	txns = make(map[string]wager.Transaction, len(u.txns.porID))
	for k, v := range u.txns.porID {
		txns[k] = v
	}
	eventos = make([]event.Event, len(u.events.publicados))
	copy(eventos, u.events.publicados)
	return wallets, lancamentos, txns, eventos
}

func (u *memUoW) restore(carteiras map[string]domainwallet.Wallet, lancamentos []ledger.Entry, txns map[string]wager.Transaction, eventos []event.Event) {
	u.wallets.porID = carteiras
	u.ledger.lancamentos = lancamentos
	u.txns.porID = txns
	u.events.publicados = eventos
}

// memTxnRepo guarda as transações escritas no caso de uso de carteira: a
// abertura nova é inserida e concluída, exatamente como no banco.
type memTxnRepo struct {
	porID map[string]wager.Transaction
}

func newMemTxnRepo() *memTxnRepo {
	return &memTxnRepo{porID: map[string]wager.Transaction{}}
}

func (r *memTxnRepo) Insert(ctx context.Context, tx *wager.Transaction) error {
	if anterior, ok := r.porID[tx.ID()]; ok {
		copia := anterior
		return &wager.Duplicate{Err: wager.ErrDuplicate, Existing: &copia, Operation: "insert"}
	}
	r.porID[tx.ID()] = *tx
	return nil
}

func (r *memTxnRepo) FindByID(ctx context.Context, id string) (*wager.Transaction, error) {
	tx, ok := r.porID[id]
	if !ok {
		return nil, wager.ErrTransactionNotFound
	}
	copia := tx
	return &copia, nil
}

func (r *memTxnRepo) FindByExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	for _, tx := range r.porID {
		if tx.ProviderID() == providerID && tx.ExternalID() == externalID {
			copia := tx
			return &copia, nil
		}
	}
	return nil, wager.ErrTransactionNotFound
}

func (r *memTxnRepo) FindByIdempotencyKey(ctx context.Context, providerID, key string) (*wager.Transaction, error) {
	for _, tx := range r.porID {
		if tx.ProviderID() == providerID && tx.IdempotencyKey() == key {
			copia := tx
			return &copia, nil
		}
	}
	return nil, wager.ErrTransactionNotFound
}

func (r *memTxnRepo) UpdateState(ctx context.Context, tx *wager.Transaction) error {
	r.porID[tx.ID()] = *tx
	return nil
}

func (r *memTxnRepo) ResolveReference(ctx context.Context, tx *wager.Transaction) error {
	r.porID[tx.ID()] = *tx
	return nil
}

func (r *memTxnRepo) ListPendingReferences(ctx context.Context, agora time.Time, limite int) ([]*wager.Transaction, error) {
	var pendentes []*wager.Transaction
	for _, tx := range r.porID {
		copia := tx
		if copia.State() == wager.StatePendingReference &&
			!copia.ReferenceNextAttempt().IsZero() &&
			!copia.ReferenceNextAttempt().After(agora) {
			pendentes = append(pendentes, &copia)
		}
	}
	if limite > 0 && len(pendentes) > limite {
		pendentes = pendentes[:limite]
	}
	return pendentes, nil
}

// memEventsRepo guarda os eventos da outbox escritos na transação.
type memEventsRepo struct {
	publicados []event.Event
	errAppend  error
}

func (r *memEventsRepo) Append(ctx context.Context, e *event.Event) error {
	if r.errAppend != nil {
		return r.errAppend
	}
	r.publicados = append(r.publicados, *e)
	return nil
}
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

	// Abertura com saldo movimenta no mesmo commit: um OPENING processado,
	// o crédito no ledger e os dois eventos do fato.
	if len(tm.uow.txns.porID) != 1 {
		t.Fatalf("OPENING não persistido: %d transações", len(tm.uow.txns.porID))
	}
	for _, op := range tm.uow.txns.porID {
		if op.Kind() != wager.KindOpening || op.State() != wager.StateProcessed {
			t.Errorf("abertura = %s/%s, quer OPENING/PROCESSED", op.Kind(), op.State())
		}
		if got := op.ObservedBalance().String(); got != "100.00" {
			t.Errorf("saldo observado da abertura = %s, quer 100.00", got)
		}
		if op.WalletID() != wlt.ID() {
			t.Errorf("abertura de %s ligada à carteira %s", op.WalletID(), wlt.ID())
		}
	}
	lancamentos, err := tm.uow.ledger.ListByWallet(context.Background(), wlt.ID(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lancamentos) != 1 {
		t.Fatalf("lançamentos = %d, quer 1", len(lancamentos))
	}
	if lancamentos[0].Direction() != ledger.Credit || lancamentos[0].Amount().String() != "100.00" {
		t.Errorf("lançamento = %s %s", lancamentos[0].Direction(), lancamentos[0].Amount())
	}
	if lancamentos[0].BalanceBefore().String() != "0.00" || lancamentos[0].BalanceAfter().String() != "100.00" {
		t.Errorf("par antes/depois = %s -> %s",
			lancamentos[0].BalanceBefore(), lancamentos[0].BalanceAfter())
	}
	if len(tm.uow.events.publicados) != 2 {
		t.Fatalf("eventos = %d, quer 2 (processamento + saldo)", len(tm.uow.events.publicados))
	}
	tipos := map[string]bool{}
	for _, evt := range tm.uow.events.publicados {
		tipos[evt.EventType()] = true
		switch evt.EventType() {
		case "WalletBalanceChanged":
			if evt.AggregateID() != wlt.ID() {
				t.Error("WalletBalanceChanged aponta para agregado errado")
			}
		case "WagerTransactionProcessed":
			if evt.AggregateID() != "" {
				// Aponta para a abertura interna, única transação gravada.
				if _, ok := tm.uow.txns.porID[evt.AggregateID()]; !ok {
					t.Error("WagerTransactionProcessed aponta para transação inexistente")
				}
			}
		}
	}
	if !tipos["WagerTransactionProcessed"] || !tipos["WalletBalanceChanged"] {
		t.Errorf("eventos esperados ausentes: %v", tipos)
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

	// Abertura sem saldo não movimenta: sem OPENING, sem lançamento e sem
	// eventos — só a carteira existe.
	if len(tm.uow.txns.porID) != 0 {
		t.Errorf("abertura zero não devia gravar OPENING, veio %d", len(tm.uow.txns.porID))
	}
	if len(tm.uow.ledger.lancamentos) != 0 {
		t.Errorf("abertura zero não devia lançar no ledger, veio %d", len(tm.uow.ledger.lancamentos))
	}
	if len(tm.uow.events.publicados) != 0 {
		t.Errorf("abertura zero não devia publicar eventos, veio %d", len(tm.uow.events.publicados))
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

func TestCreateWalletDesfazAberturaQuandoEventoFalha(t *testing.T) {
	// A publicação da outbox falha: a abertura precisa ir junto para o
	// rollback — carteira, OPENING, lançamento e o evento que já entrou.
	tm := newMemTxManager()
	tm.uow.events.errAppend = errBancoForaDoAr
	svc := wallet.NewService(tm.uow.wallets, tm.uow.ledger, tm)

	_, err := svc.CreateWallet(context.Background(), "player-1",
		money.MustParse("100.00", money.BRL), func() time.Time { return t0 })
	if !errors.Is(err, errBancoForaDoAr) {
		t.Fatalf("esperava falha da outbox, veio %v", err)
	}
	if len(tm.uow.wallets.porID) != 0 {
		t.Error("carteira sobreviveu ao rollback com evento falho")
	}
	if len(tm.uow.txns.porID) != 0 || len(tm.uow.ledger.lancamentos) != 0 {
		t.Error("OPENING ou lançamento sobreviveu ao rollback com evento falho")
	}
	if len(tm.uow.events.publicados) != 0 {
		t.Error("evento publicado sobreviveu ao rollback")
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
