package wagering_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/application/ports"
	"github.com/shimigui/go-challenge/internal/application/wagering"
	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	domainwallet "github.com/shimigui/go-challenge/internal/domain/wallet"
)

// ===== fakes em memória =====
//
// Todos os stores guardam cópias de valor, e o transactionManager fake faz
// snapshot/restore: é o que permite afirmar atomicidade — operação que falha
// não deixa nenhum rastro (carteira, ledger, transação ou evento).

type memWagerRepo struct {
	porID map[string]wager.Transaction

	errInsert     error
	errFind       error
	errUpdate     error
	updateCalls   int
	insertedCalls int
}

func newMemWagerRepo() *memWagerRepo {
	return &memWagerRepo{porID: map[string]wager.Transaction{}}
}

func (r *memWagerRepo) Insert(ctx context.Context, tx *wager.Transaction) error {
	r.insertedCalls++
	if r.errInsert != nil {
		return r.errInsert
	}
	if antigo, ok := r.porID[tx.ID()]; ok {
		copia := antigo
		return &wager.Duplicate{Err: wager.ErrDuplicate, Existing: &copia, Operation: "insert"}
	}
	r.porID[tx.ID()] = *tx
	return nil
}

func (r *memWagerRepo) FindByID(ctx context.Context, id string) (*wager.Transaction, error) {
	if r.errFind != nil {
		return nil, r.errFind
	}
	tx, ok := r.porID[id]
	if !ok {
		return nil, wager.ErrTransactionNotFound
	}
	copia := tx
	return &copia, nil
}

func (r *memWagerRepo) FindByExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	for _, tx := range r.porID {
		if tx.ProviderID() == providerID && tx.ExternalID() == externalID {
			copia := tx
			return &copia, nil
		}
	}
	return nil, wager.ErrTransactionNotFound
}

func (r *memWagerRepo) FindByIdempotencyKey(ctx context.Context, providerID, key string) (*wager.Transaction, error) {
	for _, tx := range r.porID {
		if tx.ProviderID() == providerID && tx.IdempotencyKey() == key {
			copia := tx
			return &copia, nil
		}
	}
	return nil, wager.ErrTransactionNotFound
}

func (r *memWagerRepo) UpdateState(ctx context.Context, tx *wager.Transaction) error {
	r.updateCalls++
	if r.errUpdate != nil {
		return r.errUpdate
	}
	r.porID[tx.ID()] = *tx
	return nil
}

func (r *memWagerRepo) ResolveReference(ctx context.Context, tx *wager.Transaction) error {
	r.porID[tx.ID()] = *tx
	return nil
}

func (r *memWagerRepo) ListPendingReferences(ctx context.Context, agora time.Time, limite int) ([]*wager.Transaction, error) {
	var out []*wager.Transaction
	for i := range r.porID {
		tx := r.porID[i]
		if tx.State() == wager.StatePendingReference {
			copia := tx
			out = append(out, &copia)
		}
	}
	return out, nil
}

type memWalletRepo struct {
	porID map[string]domainwallet.Wallet
}

func newMemWalletRepo() *memWalletRepo {
	return &memWalletRepo{porID: map[string]domainwallet.Wallet{}}
}

func (r *memWalletRepo) Insert(ctx context.Context, w *domainwallet.Wallet) error {
	r.porID[w.ID()] = *w
	return nil
}

func (r *memWalletRepo) UpdateBalance(ctx context.Context, w *domainwallet.Wallet) error {
	r.porID[w.ID()] = *w
	return nil
}

func (r *memWalletRepo) FindByID(ctx context.Context, id string) (*domainwallet.Wallet, error) {
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
}

func (r *memLedgerRepo) Append(ctx context.Context, e *ledger.Entry) error {
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

type memEventsRepo struct {
	eventos []event.Event
}

func (r *memEventsRepo) Append(ctx context.Context, e *event.Event) error {
	r.eventos = append(r.eventos, *e)
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

type memUoW struct {
	wallets  *memWalletRepo
	ledger   *memLedgerRepo
	txns     *memWagerRepo
	events   *memEventsRepo
	messages *memInboxRepo
}

func (u *memUoW) Wallets() domainwallet.WalletRepository { return u.wallets }
func (u *memUoW) Ledger() ledger.LedgerRepository        { return u.ledger }
func (u *memUoW) Transactions() wager.WagerTransactionRepository {
	return u.txns
}
func (u *memUoW) Events() event.OutboxRepository  { return u.events }
func (u *memUoW) Messages() ports.InboxRepository { return u.messages }

type memTxManager struct {
	uow  *memUoW
	rods int
}

func (m *memTxManager) InTransaction(ctx context.Context, fn func(ports.UnitOfWork) error) error {
	m.rods++
	carteiras, lancamentos, transacoes, eventos := m.uow.snapshot()
	if err := fn(m.uow); err != nil {
		m.uow.restore(carteiras, lancamentos, transacoes, eventos)
		return err
	}
	return nil
}

func (u *memUoW) snapshot() (map[string]domainwallet.Wallet, []ledger.Entry, map[string]wager.Transaction, []event.Event) {
	carteiras := make(map[string]domainwallet.Wallet, len(u.wallets.porID))
	for k, v := range u.wallets.porID {
		carteiras[k] = v
	}
	lancamentos := make([]ledger.Entry, len(u.ledger.lancamentos))
	copy(lancamentos, u.ledger.lancamentos)
	transacoes := make(map[string]wager.Transaction, len(u.txns.porID))
	for k, v := range u.txns.porID {
		transacoes[k] = v
	}
	eventos := make([]event.Event, len(u.events.eventos))
	copy(eventos, u.events.eventos)
	return carteiras, lancamentos, transacoes, eventos
}

func (u *memUoW) restore(carteiras map[string]domainwallet.Wallet, lancamentos []ledger.Entry, transacoes map[string]wager.Transaction, eventos []event.Event) {
	u.wallets.porID = carteiras
	u.ledger.lancamentos = lancamentos
	u.txns.porID = transacoes
	u.events.eventos = eventos
}

// ===== helpers =====

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func novoHarness() *memTxManager {
	return &memTxManager{
		uow: &memUoW{
			wallets:  newMemWalletRepo(),
			ledger:   &memLedgerRepo{},
			txns:     newMemWagerRepo(),
			events:   &memEventsRepo{},
			messages: &memInboxRepo{},
		},
	}
}

func carteiraComSaldo(t *testing.T, saldo string) *domainwallet.Wallet {
	t.Helper()
	w, err := domainwallet.New(domainwallet.Params{
		ID:       "11111111-1111-4111-8111-111111111111",
		PlayerID: "player-1",
		Opening:  money.MustParse(saldo, money.BRL),
		Now:      t0,
	})
	if err != nil {
		t.Fatalf("carteira: %v", err)
	}
	return w
}

func betParams(id string) wager.ExternalParams {
	return wager.ExternalParams{
		ID:             id,
		ProviderID:     "provider-1",
		ExternalID:     "ext-" + id,
		IdempotencyKey: "key-" + id,
		PayloadHash:    "hash-" + id,
		PlayerID:       "player-1",
		WalletID:       "11111111-1111-4111-8111-111111111111",
		Kind:           wager.KindBet,
		Amount:         money.MustParse("25.00", money.BRL),
		RoundID:        "round-1",
		GameID:         "game-1",
		Now:            t0,
	}
}

func assertSemMovimento(t *testing.T, h *memTxManager) {
	t.Helper()
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Errorf("ledger deveria estar vazio, veio %d lançamentos", len(h.uow.ledger.lancamentos))
	}
	if len(h.uow.events.eventos) != 0 {
		t.Errorf("outbox deveria estar vazio, veio %d eventos", len(h.uow.events.eventos))
	}
}

const novxs = "22222222-2222-4222-8222-222222222222"

// refUUID é a identidade da transação referenciada nos testes.
const refUUID = "33333333-3333-4333-8333-333333333333"

// ===== BET (débito) =====

func TestSubmitBetProcessaDebito(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "100.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	res, err := svc.SubmitTransaction(context.Background(), betParams(novxs))
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Replayed {
		t.Error("primeira submissão não é reentrega")
	}
	if res.Transaction == nil || res.Transaction.State() != wager.StateProcessed {
		t.Fatalf("estado = %v, quer PROCESSED", res.Transaction.State())
	}
	if res.Transaction.Kind() != wager.KindBet {
		t.Errorf("kind = %s", res.Transaction.Kind())
	}

	// Saldo debitado e persistido: 100.00 − 25.00 = 75.00.
	w, err := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Balance().String(); got != "75.00" {
		t.Errorf("saldo = %s, quer 75.00", got)
	}
	if w.Version() != 1 {
		t.Errorf("versão = %d, quer 1 (uma mutação)", w.Version())
	}

	// Lançamento DEBIT coerente com o antes/depois.
	if len(h.uow.ledger.lancamentos) != 1 {
		t.Fatalf("lancamentos = %d, quer 1", len(h.uow.ledger.lancamentos))
	}
	e := h.uow.ledger.lancamentos[0]
	if e.Direction() != ledger.Debit {
		t.Errorf("direção = %s, quer DEBIT", e.Direction())
	}
	if e.Amount().String() != "25.00" || e.BalanceBefore().String() != "100.00" || e.BalanceAfter().String() != "75.00" {
		t.Errorf("lancamento %s sobre %s -> %s", e.Amount(), e.BalanceBefore(), e.BalanceAfter())
	}
	if e.TransactionID() != novxs || e.WalletID() != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("referências do lançamento: tx=%s wallet=%s", e.TransactionID(), e.WalletID())
	}

	// Estado gravado pelo port da transação.
	stored, err := h.uow.txns.FindByID(context.Background(), novxs)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State() != wager.StateProcessed {
		t.Errorf("estado persistido = %s, quer PROCESSED", stored.State())
	}
	if stored.HasProcessedAt() == false {
		t.Error("processado não carimbado")
	}
	if !stored.HasObservedBalance() || stored.ObservedBalance().String() != "75.00" {
		t.Errorf("saldo observado persistido = %v, quer 75.00", stored.ObservedBalance())
	}
	if h.uow.txns.updateCalls != 1 {
		t.Errorf("UpdateState chamado %d vezes, quer 1", h.uow.txns.updateCalls)
	}

	// Dois eventos de domínio na mesma transação: o processamento da
	// operação e a mudança do saldo da carteira.
	if len(h.uow.events.eventos) != 2 {
		t.Fatalf("eventos = %d, quer 2", len(h.uow.events.eventos))
	}
	if h.uow.events.eventos[0].EventType() != "WagerTransactionProcessed" {
		t.Errorf("primeiro evento = %s, quer WagerTransactionProcessed", h.uow.events.eventos[0].EventType())
	}
	if h.uow.events.eventos[0].AggregateType() != "wager_transaction" || h.uow.events.eventos[0].AggregateID() != novxs {
		t.Errorf("agregado do processado = %s/%s", h.uow.events.eventos[0].AggregateType(), h.uow.events.eventos[0].AggregateID())
	}
	payload := string(h.uow.events.eventos[0].Payload())
	if !strings.Contains(payload, `"kind":"BET"`) || !strings.Contains(payload, `"observedBalance":{"amount":"75.00","currency":"BRL"}`) {
		t.Errorf("payload do processado = %s", payload)
	}

	evt := h.uow.events.eventos[1]
	if evt.EventType() != "WalletBalanceChanged" {
		t.Errorf("segundo evento = %s", evt.EventType())
	}
	if evt.AggregateType() != "wallet" || evt.AggregateID() != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("agregado = %s/%s", evt.AggregateType(), evt.AggregateID())
	}
	if evt.Version() != 1 {
		t.Errorf("versão do evento = %d, quer 1", evt.Version())
	}
	payload = string(evt.Payload())
	if !strings.Contains(payload, `"walletId":"11111111-1111-4111-8111-111111111111"`) ||
		!strings.Contains(payload, `"balanceBefore":{"amount":"100.00","currency":"BRL"}`) ||
		!strings.Contains(payload, `"balanceAfter":{"amount":"75.00","currency":"BRL"}`) ||
		!strings.Contains(payload, `"walletVersion":1`) {
		t.Errorf("payload = %s", payload)
	}
}

func TestSubmitBetSaldoInsuficienteRejeitaCommitado(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Amount = money.MustParse("80.00", money.BRL)

	// A recusa é resposta de negócio, não erro de infraestrutura: a
	// transação vira REJECTED persistida com o código estável e o evento é
	// publicado — uma reentrega responde a mesma recusa.
	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("rejeição deveria vir como resultado, veio erro %v", err)
	}
	if res.Replayed {
		t.Error("primeira submissão não é reentrega")
	}
	if res.Transaction.State() != wager.StateRejected {
		t.Fatalf("estado = %s, quer REJECTED", res.Transaction.State())
	}
	if res.Transaction.FailureCode() != "INSUFFICIENT_FUNDS" {
		t.Errorf("failureCode = %s, quer INSUFFICIENT_FUNDS", res.Transaction.FailureCode())
	}

	// Rejeição commitada: a operação existe, o saldo não muda, o ledger
	// continua vazio e um evento de recusa foi publicado.
	stored, err := h.uow.txns.FindByID(context.Background(), novxs)
	if err != nil {
		t.Fatalf("transação rejeitada deveria estar persistida: %v", err)
	}
	if stored.State() != wager.StateRejected || stored.FailureCode() != "INSUFFICIENT_FUNDS" {
		t.Errorf("estado persistido = %s/%s", stored.State(), stored.FailureCode())
	}

	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("saldo após rejeição = %s, quer 50.00", got)
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Errorf("rejeição não pode lançar no ledger, veio %d", len(h.uow.ledger.lancamentos))
	}
	if len(h.uow.events.eventos) != 1 {
		t.Fatalf("eventos = %d, quer 1 (recusa)", len(h.uow.events.eventos))
	}
	evt := h.uow.events.eventos[0]
	if evt.EventType() != "WagerTransactionRejected" {
		t.Errorf("eventType = %s, quer WagerTransactionRejected", evt.EventType())
	}
	if !strings.Contains(string(evt.Payload()), `"failureCode":"INSUFFICIENT_FUNDS"`) {
		t.Errorf("payload = %s", evt.Payload())
	}
}

func TestSubmitBetCarteiraInexistenteDesfazTudo(t *testing.T) {
	h := novoHarness()
	svc := wagering.NewService(h.uow.txns, h)

	_, err := svc.SubmitTransaction(context.Background(), betParams(novxs))
	if !errors.Is(err, domainwallet.ErrWalletNotFound) {
		t.Fatalf("esperava ErrWalletNotFound, veio %v", err)
	}
	assertSemMovimento(t, h)
	if _, err := h.uow.txns.FindByID(context.Background(), novxs); !errors.Is(err, wager.ErrTransactionNotFound) {
		t.Errorf("carteira inexistente deveria desfazer a transação, veio %v", err)
	}
}

// ===== WIN (crédito) =====

func TestSubmitWinCreditaSaldo(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "0.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindWin
	params.Amount = money.MustParse("25.00", money.BRL)

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.Kind() != wager.KindWin || res.Transaction.State() != wager.StateProcessed {
		t.Fatalf("resultado = %s/%s", res.Transaction.Kind(), res.Transaction.State())
	}

	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "25.00" {
		t.Errorf("saldo = %s, quer 25.00", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 || h.uow.ledger.lancamentos[0].Direction() != ledger.Credit {
		t.Errorf("esperava um CREDIT, veio %d lançamentos", len(h.uow.ledger.lancamentos))
	}
	if !res.Transaction.HasObservedBalance() || res.Transaction.ObservedBalance().String() != "25.00" {
		t.Errorf("saldo observado = %v, quer 25.00", res.Transaction.ObservedBalance())
	}
}

// ===== LOSS (sem movimento, com saldo observado) =====

func TestSubmitLossNaoMoveSaldoMasObserva(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindLoss
	params.Amount = money.Zero(money.BRL)

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StateProcessed {
		t.Errorf("estado = %s, quer PROCESSED", res.Transaction.State())
	}
	if !res.Transaction.HasObservedBalance() || res.Transaction.ObservedBalance().String() != "50.00" {
		t.Errorf("LOSS deveria carregar o saldo observado, veio %v", res.Transaction.ObservedBalance())
	}

	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("LOSS não pode mover saldo, veio %s", got)
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Errorf("LOSS não pode lançar no ledger, veio %d", len(h.uow.ledger.lancamentos))
	}
	if h.uow.txns.updateCalls != 1 {
		t.Errorf("UpdateState deveria encerrar o LOSS, veio %d chamadas", h.uow.txns.updateCalls)
	}

	// LOSS publica o WagerTransactionProcessed (com saldo observado) e não
	// publica WalletBalanceChanged: nenhuma alteração de saldo aconteceu.
	if len(h.uow.events.eventos) != 1 {
		t.Fatalf("eventos = %d, quer 1", len(h.uow.events.eventos))
	}
	evt := h.uow.events.eventos[0]
	if evt.EventType() != "WagerTransactionProcessed" {
		t.Errorf("eventType = %s, quer WagerTransactionProcessed", evt.EventType())
	}
	if !strings.Contains(string(evt.Payload()), `"kind":"LOSS"`) ||
		!strings.Contains(string(evt.Payload()), `"observedBalance":{"amount":"50.00","currency":"BRL"}`) {
		t.Errorf("payload do LOSS = %s", evt.Payload())
	}
}

func TestSubmitLossComValorRejeitado(t *testing.T) {
	h := novoHarness()
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindLoss
	params.Amount = money.MustParse("1.00", money.BRL)

	_, err := svc.SubmitTransaction(context.Background(), params)
	if !errors.Is(err, wager.ErrInvalidAmountForKind) {
		t.Fatalf("LOSS com valor: esperava ErrInvalidAmountForKind, veio %v", err)
	}
	assertSemMovimento(t, h)
}

// apostaProcessada grava no harness uma aposta concluída, pronta para
// servir de referência a REFUND/ROLLBACK.
func apostaProcessada(t *testing.T, h *memTxManager, id string) *wager.Transaction {
	t.Helper()
	tx := mustTransactionRevertido(t, id)
	if err := tx.SetObservedBalance(money.MustParse("75.00", money.BRL)); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkProcessed(t0); err != nil {
		t.Fatal(err)
	}
	if err := h.uow.txns.Insert(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	return tx
}

// vitoriaProcessada grava uma WIN concluída, referência de um ROLLBACK.
func vitoriaProcessada(t *testing.T, h *memTxManager, id string) *wager.Transaction {
	t.Helper()
	tx := mustTransactionRevertido(t, id)
	tx2, err := wager.NewExternal(wager.ExternalParams{
		ID: tx.ID(), ProviderID: "provider-1", ExternalID: "ext-" + id,
		IdempotencyKey: "key-" + id, PayloadHash: "hash-" + id,
		PlayerID: "player-1", WalletID: "11111111-1111-4111-8111-111111111111",
		Kind: wager.KindWin, Amount: money.MustParse("25.00", money.BRL),
		RoundID: "round-1", GameID: "game-1", Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx2.SetObservedBalance(money.MustParse("125.00", money.BRL)); err != nil {
		t.Fatal(err)
	}
	if err := tx2.MarkProcessed(t0); err != nil {
		t.Fatal(err)
	}
	if err := h.uow.txns.Insert(context.Background(), tx2); err != nil {
		t.Fatal(err)
	}
	return tx2
}

// ===== REFUND (crédito com referência) =====

func TestSubmitRefundCreditaComReferencia(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	apostaProcessada(t, h, "33333333-3333-4333-8333-333333333333")
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StateProcessed {
		t.Errorf("estado = %s", res.Transaction.State())
	}
	if res.Transaction.ReferenceID() == "" {
		t.Error("referência interna deveria estar resolvida")
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "35.00" {
		t.Errorf("saldo = %s, quer 35.00 (10.00 + 25.00)", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 || h.uow.ledger.lancamentos[0].Direction() != ledger.Credit {
		t.Errorf("esperava um CREDIT do refund, veio %d lançamentos", len(h.uow.ledger.lancamentos))
	}
	if len(h.uow.events.eventos) != 2 {
		t.Errorf("refund deveria publicar processamento + saldo, veio %d eventos", len(h.uow.events.eventos))
	}

	// A resolução da referência foi persistida junto com a conclusão.
	stored, err := h.uow.txns.FindByID(context.Background(), novxs)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ReferenceID() != "33333333-3333-4333-8333-333333333333" {
		t.Errorf("referência persistida = %q, quer tx-ref", stored.ReferenceID())
	}
}

func TestSubmitRefundReferenciaAindaPendenteEspera(t *testing.T) {
	// A referência chegou (a aposta existe) mas ainda não foi processada: a
	// reversão espera em PENDING_REFERENCE, não opera às cegas.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	if err := h.uow.txns.Insert(context.Background(), mustTransactionRevertido(t, "33333333-3333-4333-8333-333333333333")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StatePendingReference {
		t.Fatalf("estado = %s, quer PENDING_REFERENCE", res.Transaction.State())
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Error("pendência não pode lançar no ledger")
	}
	if len(h.uow.events.eventos) != 1 || h.uow.events.eventos[0].EventType() != "WagerTransactionPendingReference" {
		t.Errorf("esperava o evento de espera, veio %d eventos", len(h.uow.events.eventos))
	}
}

func TestSubmitRefundSemReferenciaNaoEncontradaEspera(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-que-nao-existe"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StatePendingReference {
		t.Fatalf("estado = %s, quer PENDING_REFERENCE", res.Transaction.State())
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Error("pendência não pode lançar no ledger")
	}
	// Prazo inicial baseado na submissão, para o worker retomar depois.
	if res.Transaction.ReferenceNextAttempt().IsZero() {
		t.Error("pendência sem prazo de retomada")
	}
}

func TestSubmitRefundReferenciaIncompativelRejeita(t *testing.T) {
	// Referência de outro jogador: REFERENCE_MISMATCH, recusa persistida.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	aposta := mustTransactionRevertido(t, "33333333-3333-4333-8333-333333333333")
	aposta2, err := wager.NewExternal(wager.ExternalParams{
		ID: aposta.ID(), ProviderID: "provider-1", ExternalID: "ext-33333333-3333-4333-8333-333333333333",
		IdempotencyKey: "key-33333333-3333-4333-8333-333333333333", PayloadHash: "hash-33333333-3333-4333-8333-333333333333",
		PlayerID: "outro-jogador", WalletID: "11111111-1111-4111-8111-111111111111",
		Kind: wager.KindBet, Amount: money.MustParse("25.00", money.BRL),
		RoundID: "round-1", GameID: "game-1", Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := aposta2.MarkProcessed(t0); err != nil {
		t.Fatal(err)
	}
	if err := h.uow.txns.Insert(context.Background(), aposta2); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StateRejected {
		t.Fatalf("estado = %s, quer REJECTED", res.Transaction.State())
	}
	if res.Transaction.FailureCode() != "REFERENCE_MISMATCH" {
		t.Errorf("failureCode = %s, quer REFERENCE_MISMATCH", res.Transaction.FailureCode())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "10.00" {
		t.Errorf("rejeição movimentou saldo: %s", got)
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Error("rejeição não pode lançar no ledger")
	}
	if len(h.uow.events.eventos) != 1 || h.uow.events.eventos[0].EventType() != "WagerTransactionRejected" {
		t.Errorf("esperava o evento de recusa, veio %d eventos", len(h.uow.events.eventos))
	}
}

func TestSubmitRefundSemReferenciaRejeitado(t *testing.T) {
	h := novoHarness()
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = ""

	_, err := svc.SubmitTransaction(context.Background(), params)
	if !errors.Is(err, wager.ErrMissingReference) {
		t.Fatalf("esperava ErrMissingReference, veio %v", err)
	}
	assertSemMovimento(t, h)
}

// ===== ROLLBACK (reversão com referência resolvida) =====

func TestSubmitRollbackSemReferenciaEspera(t *testing.T) {
	// ROLLBACK pede a referência: sem ela (ainda não chegou), a operação
	// espera em PENDING_REFERENCE — nunca movimenta saldo às cegas.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRollback
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-bet-original"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StatePendingReference {
		t.Errorf("estado = %s, quer PENDING_REFERENCE", res.Transaction.State())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("saldo = %s, quer 50.00", got)
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Error("pendência não pode lançar no ledger")
	}
}

func TestSubmitRollbackDeApostaCredita(t *testing.T) {
	// Desfazer um BET credita o valor apostado.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	apostaProcessada(t, h, "33333333-3333-4333-8333-333333333333")
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRollback
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StateProcessed {
		t.Fatalf("estado = %s, quer PROCESSED", res.Transaction.State())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "75.00" {
		t.Errorf("saldo = %s, quer 75.00 (50.00 + 25.00)", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 || h.uow.ledger.lancamentos[0].Direction() != ledger.Credit {
		t.Errorf("esperava um CREDIT do rollback de aposta, veio %d lançamentos", len(h.uow.ledger.lancamentos))
	}
}

func TestSubmitRollbackDeWinDebita(t *testing.T) {
	// Desfazer um WIN debita o prêmio pago.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	vitoriaProcessada(t, h, "33333333-3333-4333-8333-333333333333")
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRollback
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StateProcessed {
		t.Fatalf("estado = %s, quer PROCESSED", res.Transaction.State())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "25.00" {
		t.Errorf("saldo = %s, quer 25.00 (50.00 - 25.00)", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 || h.uow.ledger.lancamentos[0].Direction() != ledger.Debit {
		t.Errorf("esperava um DEBIT do rollback de win, veio %d lançamentos", len(h.uow.ledger.lancamentos))
	}
}

func TestSubmitRollbackDeWinSaldoInsuficienteRejeita(t *testing.T) {
	// Desfazer um WIN de 25.00 com saldo atual de 10.00: a reversão não
	// pode levar o saldo a negativo, então é recusa persistida com o código
	// específico de reversão.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	vitoriaProcessada(t, h, "33333333-3333-4333-8333-333333333333")
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRollback
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StateRejected {
		t.Fatalf("estado = %s, quer REJECTED", res.Transaction.State())
	}
	if res.Transaction.FailureCode() != "INSUFFICIENT_FUNDS_REVERSAL" {
		t.Errorf("failureCode = %s, quer INSUFFICIENT_FUNDS_REVERSAL", res.Transaction.FailureCode())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "10.00" {
		t.Errorf("saldo = %s, quer 10.00 (rejeição não move)", got)
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Error("rejeição não pode lançar no ledger")
	}
	if len(h.uow.events.eventos) != 1 || h.uow.events.eventos[0].EventType() != "WagerTransactionRejected" {
		t.Errorf("esperava o evento de recusa, veio %d eventos", len(h.uow.events.eventos))
	}
}

func TestSubmitRollbackReferenciaRejeitadaEspera(t *testing.T) {
	// A referência existe, mas foi rejeitada (nunca processada): a reversão
	// espera — o provedor pode estar reenviando a aposta — e é a exaustão
	// de tentativas do worker que encerra com REFERENCE_NOT_FOUND.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	aposta := mustTransactionRevertido(t, "33333333-3333-4333-8333-333333333333")
	if err := aposta.MarkRejected("INSUFFICIENT_FUNDS", "saldo insuficiente", t0); err != nil {
		t.Fatal(err)
	}
	if err := h.uow.txns.Insert(context.Background(), aposta); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRollback
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StatePendingReference {
		t.Fatalf("estado = %s, quer PENDING_REFERENCE", res.Transaction.State())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("saldo = %s, quer 50.00", got)
	}
}

// ===== entradas inválidas =====

func TestSubmitValoresErradosRejeitados(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*wager.ExternalParams)
		want   error
	}{
		{"valor negativo no BET", func(p *wager.ExternalParams) {
			p.Amount = money.MustNew(-25, money.BRL)
		}, wager.ErrInvalidAmountForKind},
		{"valor zero no BET", func(p *wager.ExternalParams) {
			p.Amount = money.Zero(money.BRL)
		}, wager.ErrInvalidAmountForKind},
		{"OPENING vem de fora", func(p *wager.ExternalParams) {
			p.Kind = wager.KindOpening
		}, wager.ErrExternalNotAllowed},
		{"provider vazio", func(p *wager.ExternalParams) {
			p.ProviderID = ""
		}, wager.ErrInvalidProviderID},
		{"id externo vazio", func(p *wager.ExternalParams) {
			p.ExternalID = ""
		}, wager.ErrInvalidExternalID},
		{"chave de idempotência vazia", func(p *wager.ExternalParams) {
			p.IdempotencyKey = ""
		}, wager.ErrInvalidIdempotencyKey},
		{"payload hash vazio", func(p *wager.ExternalParams) {
			p.PayloadHash = ""
		}, wager.ErrInvalidPayloadHash},
		{"carteira vazia", func(p *wager.ExternalParams) {
			p.WalletID = ""
		}, identifier.ErrInvalidWalletID},
		{"jogador vazio", func(p *wager.ExternalParams) {
			p.PlayerID = ""
		}, identifier.ErrInvalidPlayerID},
		{"tipo inexistente", func(p *wager.ExternalParams) {
			p.Kind = wager.Kind("TRAP")
		}, wager.ErrInvalidKind},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := novoHarness()
			svc := wagering.NewService(h.uow.txns, h)

			params := betParams(novxs)
			c.mutate(&params)

			_, err := svc.SubmitTransaction(context.Background(), params)
			if !errors.Is(err, c.want) {
				t.Fatalf("esperava %v, veio %v", c.want, err)
			}
			assertSemMovimento(t, h)
		})
	}
}

// ===== idempotência (reentrega do provedor) =====

func TestSubmitReentregaIdempotente(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "100.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	primeiro, err := svc.SubmitTransaction(context.Background(), betParams(novxs))
	if err != nil {
		t.Fatalf("primeira submissão: %v", err)
	}
	if primeiro.Replayed {
		t.Error("primeira não pode ser reentrega")
	}

	// Mesma identidade, mesmo payload (hash igual): resposta é o registro
	// anterior, sem reprocessar — o saldo não é debitado de novo.
	segundo, err := svc.SubmitTransaction(context.Background(), betParams(novxs))
	if err != nil {
		t.Fatalf("reentrega: %v", err)
	}
	if !segundo.Replayed {
		t.Error("reentrega deveria vir com Replayed=true")
	}
	if got := segundo.Transaction.ID(); got != novxs {
		t.Errorf("resposta deveria ser o registro anterior, veio %s", got)
	}
	if !segundo.Transaction.HasProcessedAt() {
		t.Error("registro anterior é processado")
	}

	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "75.00" {
		t.Errorf("reentrega movimentou saldo: %s, quer 75.00", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 {
		t.Errorf("reentrega gravou %d lançamentos, quer 1", len(h.uow.ledger.lancamentos))
	}
	if len(h.uow.events.eventos) != 2 {
		t.Errorf("reentrega publicou %d eventos, quer 2", len(h.uow.events.eventos))
	}
}

func TestSubmitReentregaComPayloadDiferenteConflita(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "100.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	if _, err := svc.SubmitTransaction(context.Background(), betParams(novxs)); err != nil {
		t.Fatalf("primeira submissão: %v", err)
	}

	diferente := betParams(novxs)
	diferente.PayloadHash = "hash-modificado"
	diferente.Amount = money.MustParse("30.00", money.BRL)

	_, err := svc.SubmitTransaction(context.Background(), diferente)
	if !errors.Is(err, wager.ErrIdempotencyConflict) {
		t.Fatalf("esperava ErrIdempotencyConflict, veio %v", err)
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "75.00" {
		t.Errorf("conflito movimentou saldo: %s", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 {
		t.Errorf("conflito gravou %d lançamentos", len(h.uow.ledger.lancamentos))
	}
}

func TestSubmitReentregaComCarteiraRemovidaAindaResponde(t *testing.T) {
	// A reentrega nem consulta a carteira: responde com o registro salvo.
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "100.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	if _, err := svc.SubmitTransaction(context.Background(), betParams(novxs)); err != nil {
		t.Fatalf("primeira submissão: %v", err)
	}
	// A carteira foi removida depois do processamento.
	delete(h.uow.wallets.porID, "11111111-1111-4111-8111-111111111111")

	segundo, err := svc.SubmitTransaction(context.Background(), betParams(novxs))
	if err != nil {
		t.Fatalf("reentrega: %v", err)
	}
	if !segundo.Replayed || segundo.Transaction.State() != wager.StateProcessed {
		t.Errorf("esperava replay processado, veio replayed=%v estado=%s",
			segundo.Replayed, segundo.Transaction.State())
	}
	if !segundo.Transaction.HasObservedBalance() {
		t.Error("replay deveria devolver o saldo observado do registro")
	}
}

// ===== worker de referências (retomada da pendência) =====

func TestWorkerResolvePendenciaQuandoReferenciaChega(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	// Submissão sem a referência: fica PENDING_REFERENCE.
	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-33333333-3333-4333-8333-333333333333"
	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if res.Transaction.State() != wager.StatePendingReference {
		t.Fatalf("estado = %s, quer PENDING_REFERENCE", res.Transaction.State())
	}

	// A aposta referenciada chega e é processada depois.
	apostaProcessada(t, h, "33333333-3333-4333-8333-333333333333")

	// O worker retoma a pendência e conclui a reversão.
	worker := wagering.NewReferenceWorker(h.uow.txns, h)
	worker.SetNow(func() time.Time { return t0.Add(2 * time.Second) })

	resolvidas, err := worker.Retomar(context.Background())
	if err != nil {
		t.Fatalf("Retomar: %v", err)
	}
	if resolvidas != 1 {
		t.Errorf("resolvidas = %d, quer 1", resolvidas)
	}

	stored, err := h.uow.txns.FindByID(context.Background(), novxs)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State() != wager.StateProcessed {
		t.Fatalf("estado após retomada = %s, quer PROCESSED", stored.State())
	}
	if stored.ReferenceID() != "33333333-3333-4333-8333-333333333333" {
		t.Errorf("referência resolvida = %q, quer tx-ref", stored.ReferenceID())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "35.00" {
		t.Errorf("saldo = %s, quer 35.00 (10.00 + 25.00)", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 {
		t.Errorf("ledger = %d lançamentos, quer 1", len(h.uow.ledger.lancamentos))
	}
}

func TestWorkerContaTentativasComBackoff(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-que-nao-chega"
	if _, err := svc.SubmitTransaction(context.Background(), params); err != nil {
		t.Fatal(err)
	}

	worker := wagering.NewReferenceWorker(h.uow.txns, h)
	agora := t0.Add(2 * time.Second)
	worker.SetNow(func() time.Time { return agora })

	// Primeira retomada sem a referência: conta a tentativa e agenda o
	// backoff exponencial (2s → 4s → 8s → 16s).
	resolvidas, err := worker.Retomar(context.Background())
	if err != nil {
		t.Fatalf("Retomar: %v", err)
	}
	if resolvidas != 0 {
		t.Errorf("referência ausente não resolve nada, veio %d", resolvidas)
	}
	stored, _ := h.uow.txns.FindByID(context.Background(), novxs)
	if stored.State() != wager.StatePendingReference {
		t.Fatalf("estado = %s, quer PENDING_REFERENCE", stored.State())
	}
	if stored.ReferenceAttempts() != 1 {
		t.Errorf("tentativas = %d, quer 1", stored.ReferenceAttempts())
	}
	esperado := agora.Add(2 * time.Second)
	if !stored.ReferenceNextAttempt().Equal(esperado) {
		t.Errorf("próxima tentativa = %s, quer %s", stored.ReferenceNextAttempt(), esperado)
	}
}

func TestWorkerEsgotaTentativasERejeita(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-que-nao-chega"
	if _, err := svc.SubmitTransaction(context.Background(), params); err != nil {
		t.Fatal(err)
	}

	worker := wagering.NewReferenceWorker(h.uow.txns, h)

	// O limite é de 5 tentativas: 1 (submissão) + 4 retomadas do worker.
	for i := 0; i < 5; i++ {
		resolvidas, err := worker.Retomar(context.Background())
		if err != nil {
			t.Fatalf("Retomar %d: %v", i, err)
		}
		if i < 4 && resolvidas != 0 {
			t.Fatalf("retomada %d não deveria resolver, veio %d", i, resolvidas)
		}
		if i == 4 && resolvidas != 1 {
			t.Fatalf("última retomada deveria resolver (rejeitar), veio %d", resolvidas)
		}
	}

	stored, err := h.uow.txns.FindByID(context.Background(), novxs)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State() != wager.StateRejected {
		t.Fatalf("estado final = %s, quer REJECTED", stored.State())
	}
	if stored.FailureCode() != "REFERENCE_NOT_FOUND" {
		t.Errorf("failureCode = %s, quer REFERENCE_NOT_FOUND", stored.FailureCode())
	}
	if len(h.uow.ledger.lancamentos) != 0 {
		t.Error("rejeição por esgotamento não pode lançar no ledger")
	}
	if len(h.uow.events.eventos) != 2 {
		t.Errorf("eventos = %d, quer 2 (espera + recusa)", len(h.uow.events.eventos))
	}
	if h.uow.events.eventos[1].EventType() != "WagerTransactionRejected" {
		t.Errorf("último evento = %s, quer WagerTransactionRejected", h.uow.events.eventos[1].EventType())
	}
}

// ===== consultas =====

func TestGetTransaction(t *testing.T) {
	h := novoHarness()
	svc := wagering.NewService(h.uow.txns, h)

	if err := h.uow.txns.Insert(context.Background(), mustTransactionRevertido(t, novxs)); err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetTransaction(context.Background(), novxs)
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if got.ID() != novxs {
		t.Errorf("id = %s", got.ID())
	}
}

func TestGetTransactionNaoEncontrada(t *testing.T) {
	h := novoHarness()
	svc := wagering.NewService(h.uow.txns, h)

	if _, err := svc.GetTransaction(context.Background(), "tx-zzz"); !errors.Is(err, wager.ErrTransactionNotFound) {
		t.Fatalf("esperava ErrTransactionNotFound, veio %v", err)
	}
}

func TestGetTransactionByExternal(t *testing.T) {
	h := novoHarness()
	svc := wagering.NewService(h.uow.txns, h)

	if err := h.uow.txns.Insert(context.Background(), mustTransactionRevertido(t, novxs)); err != nil {
		t.Fatal(err)
	}

	got, err := svc.GetTransactionByExternal(context.Background(), "provider-1", "ext-"+novxs)
	if err != nil {
		t.Fatalf("GetTransactionByExternal: %v", err)
	}
	if got.ExternalID() != "ext-"+novxs {
		t.Errorf("externalId = %s", got.ExternalID())
	}
}

func mustTransactionRevertido(t *testing.T, id string) *wager.Transaction {
	t.Helper()
	tx, err := wager.NewExternal(wager.ExternalParams{
		ID:             id,
		ProviderID:     "provider-1",
		ExternalID:     "ext-" + id,
		IdempotencyKey: "key-" + id,
		PayloadHash:    "hash-" + id,
		PlayerID:       "player-1",
		WalletID:       "11111111-1111-4111-8111-111111111111",
		Kind:           wager.KindBet,
		Amount:         money.MustParse("25.00", money.BRL),
		RoundID:        "round-1",
		GameID:         "game-1",
		Now:            t0,
	})
	if err != nil {
		t.Fatalf("transação: %v", err)
	}
	return tx
}
