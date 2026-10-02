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

const novxs = "tx-1"

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
	if w.Version() != 2 {
		t.Errorf("versão = %d, quer 2 (uma mutação)", w.Version())
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
	if h.uow.txns.updateCalls != 1 {
		t.Errorf("UpdateState chamado %d vezes, quer 1", h.uow.txns.updateCalls)
	}

	// Evento de domínio publicado via outbox na mesma transação.
	if len(h.uow.events.eventos) != 1 {
		t.Fatalf("eventos = %d, quer 1", len(h.uow.events.eventos))
	}
	evt := h.uow.events.eventos[0]
	if evt.EventType() != "WalletBalanceChanged" {
		t.Errorf("eventType = %s", evt.EventType())
	}
	if evt.AggregateType() != "wallet" || evt.AggregateID() != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("agregado = %s/%s", evt.AggregateType(), evt.AggregateID())
	}
	if evt.Version() != 1 {
		t.Errorf("versão do evento = %d, quer 1", evt.Version())
	}
	payload := string(evt.Payload())
	if !strings.Contains(payload, `"walletId":"11111111-1111-4111-8111-111111111111"`) || !strings.Contains(payload, `"balance":"75.00"`) {
		t.Errorf("payload = %s", payload)
	}
}

func TestSubmitBetSaldoInsuficienteRolaBack(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Amount = money.MustParse("80.00", money.BRL)

	_, err := svc.SubmitTransaction(context.Background(), params)
	if !errors.Is(err, domainwallet.ErrInsufficientFunds) {
		t.Fatalf("esperava ErrInsufficientFunds, veio %v", err)
	}

	// Rollback: saldo intacto, nada no ledger/outbox, transação não persiste.
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("saldo após rollback = %s, quer 50.00", got)
	}
	assertSemMovimento(t, h)
	if _, err := h.uow.txns.FindByID(context.Background(), novxs); !errors.Is(err, wager.ErrTransactionNotFound) {
		t.Errorf("transação deveria ter desfeito o insert, veio %v", err)
	}
	if h.uow.txns.updateCalls != 0 {
		t.Errorf("UpdateState não deveria ser chamado, veio %d", h.uow.txns.updateCalls)
	}
}

func TestSubmitBetCarteiraInexistente(t *testing.T) {
	h := novoHarness()
	svc := wagering.NewService(h.uow.txns, h)

	_, err := svc.SubmitTransaction(context.Background(), betParams(novxs))
	if !errors.Is(err, domainwallet.ErrWalletNotFound) {
		t.Fatalf("esperava ErrWalletNotFound, veio %v", err)
	}
	assertSemMovimento(t, h)
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
}

// ===== LOSS (sem movimento) =====

func TestSubmitLossNaoMoveSaldo(t *testing.T) {
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

	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("LOSS não pode mover saldo, veio %s", got)
	}
	assertSemMovimento(t, h)
	if h.uow.txns.updateCalls != 1 {
		t.Errorf("UpdateState deveria encerrar o LOSS, veio %d chamadas", h.uow.txns.updateCalls)
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

// ===== REFUND (crédito com referência) =====

func TestSubmitRefundCreditaComReferencia(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "10.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRefund
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-bet-original"

	res, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("SubmitTransaction: %v", err)
	}
	if res.Transaction.State() != wager.StateProcessed {
		t.Errorf("estado = %s", res.Transaction.State())
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "35.00" {
		t.Errorf("saldo = %s, quer 35.00 (10.00 + 25.00)", got)
	}
	if len(h.uow.ledger.lancamentos) != 1 || h.uow.ledger.lancamentos[0].Direction() != ledger.Credit {
		t.Errorf("esperava um CREDIT do refund, veio %d lançamentos", len(h.uow.ledger.lancamentos))
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

// ===== ROLLBACK (reversão não resolvida: erro) =====

func TestSubmitRollbackSemReferenciaResolvidaErra(t *testing.T) {
	h := novoHarness()
	if err := h.uow.wallets.Insert(context.Background(), carteiraComSaldo(t, "50.00")); err != nil {
		t.Fatal(err)
	}
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindRollback
	params.Amount = money.MustParse("25.00", money.BRL)
	params.ReferenceExtID = "ext-bet-original"

	// A reversão pede a referência resolvida (worker de referências); sem
	// ela, o sentido do movimento não existe e a submissão recusa — nunca
	// movimenta saldo às cegas.
	_, err := svc.SubmitTransaction(context.Background(), params)
	if err == nil {
		t.Fatal("ROLLBACK sem referência resolvida deveria falhar")
	}
	if !errors.Is(err, wager.ErrInvalidKind) {
		t.Fatalf("esperava ErrInvalidKind, veio %v", err)
	}
	w, _ := h.uow.wallets.FindByID(context.Background(), "11111111-1111-4111-8111-111111111111")
	if got := w.Balance().String(); got != "50.00" {
		t.Errorf("saldo = %s, quer 50.00 (rollback desfeito)", got)
	}
	assertSemMovimento(t, h)
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
	if len(h.uow.events.eventos) != 1 {
		t.Errorf("reentrega publicou %d eventos, quer 1", len(h.uow.events.eventos))
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
	svc := wagering.NewService(h.uow.txns, h)

	params := betParams(novxs)
	params.Kind = wager.KindLoss
	params.Amount = money.Zero(money.BRL)

	if _, err := svc.SubmitTransaction(context.Background(), params); err != nil {
		t.Fatalf("LOSS sem carteira deveria processar (não move): %v", err)
	}
	segundo, err := svc.SubmitTransaction(context.Background(), params)
	if err != nil {
		t.Fatalf("reentrega: %v", err)
	}
	if !segundo.Replayed || segundo.Transaction.State() != wager.StateProcessed {
		t.Errorf("esperava replay processado, veio replayed=%v estado=%s",
			segundo.Replayed, segundo.Transaction.State())
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

	got, err := svc.GetTransactionByExternal(context.Background(), "provider-1", "ext-tx-1")
	if err != nil {
		t.Fatalf("GetTransactionByExternal: %v", err)
	}
	if got.ExternalID() != "ext-tx-1" {
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
