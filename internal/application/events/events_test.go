package events_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/application/events"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
)

var (
	t0       = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	walletID = "11111111-1111-1111-1111-111111111111"
	playerID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	txID     = "22222222-2222-2222-2222-222222222222"
	brl      = money.MustParse
)

func transacaoProcessada(t *testing.T) *wager.Transaction {
	t.Helper()
	tx, err := wager.NewExternal(wager.ExternalParams{
		ID: txID, ProviderID: "prov-1", ExternalID: "ext-1",
		IdempotencyKey: "prov-1:ext-1", PayloadHash: "hash-1",
		PlayerID: playerID, WalletID: walletID,
		Kind: wager.KindBet, Amount: brl("25.00", money.BRL),
		RoundID: "rodada-1", Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetObservedBalance(brl("75.00", money.BRL)); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkProcessed(t0); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestWagerTransactionProcessedCarregaSnapshot(t *testing.T) {
	payload, err := events.WagerTransactionProcessed(transacaoProcessada(t))
	if err != nil {
		t.Fatal(err)
	}

	var got events.ProcessedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.TransactionID != txID || got.Kind != "BET" || got.Status != "PROCESSED" {
		t.Errorf("identidade errada: %+v", got)
	}
	if got.Money.Amount != "25.00" || got.Money.Currency != "BRL" {
		t.Errorf("dinheiro errado: %+v", got.Money)
	}
	if got.ObservedBalance == nil || got.ObservedBalance.Amount != "75.00" {
		t.Errorf("saldo observado ausente ou errado: %+v", got.ObservedBalance)
	}
	if got.PlayerID != playerID || got.WalletID != walletID {
		t.Error("aggregate fora do payload")
	}
	if !got.ProcessedAt.Equal(t0) {
		t.Errorf("processedAt = %s", got.ProcessedAt)
	}
}

func TestWagerTransactionProcessedSemSaldoObservadoOmitido(t *testing.T) {
	tx, err := wager.NewExternal(wager.ExternalParams{
		ID: txID, ProviderID: "prov-1", ExternalID: "ext-1",
		IdempotencyKey: "prov-1:ext-1", PayloadHash: "hash-1",
		PlayerID: playerID, WalletID: walletID,
		Kind: wager.KindBet, Amount: brl("10.00", money.BRL), Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := events.WagerTransactionProcessed(tx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "observedBalance") {
		t.Errorf("payload pendente não pode anunciar saldo: %s", payload)
	}
}

func TestWagerTransactionRejectedCarregaFalha(t *testing.T) {
	tx, _ := wager.NewExternal(wager.ExternalParams{
		ID: txID, ProviderID: "prov-1", ExternalID: "ext-1",
		IdempotencyKey: "prov-1:ext-1", PayloadHash: "hash-1",
		PlayerID: playerID, WalletID: walletID,
		Kind: wager.KindBet, Amount: brl("50.00", money.BRL), Now: t0,
	})
	if err := tx.MarkRejected("INSUFFICIENT_FUNDS", "saldo insuficiente", t0); err != nil {
		t.Fatal(err)
	}
	payload, err := events.WagerTransactionRejected(tx)
	if err != nil {
		t.Fatal(err)
	}
	var got events.RejectedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "REJECTED" || got.FailureCode != "INSUFFICIENT_FUNDS" {
		t.Errorf("falha errada: %+v", got)
	}
	if got.Money.Amount != "50.00" {
		t.Errorf("dinheiro errado: %+v", got.Money)
	}
}

func TestWagerTransactionPendingReferenceCarregaPrazo(t *testing.T) {
	tx, _ := wager.NewExternal(wager.ExternalParams{
		ID: txID, ProviderID: "prov-1", ExternalID: "ext-1",
		IdempotencyKey: "prov-1:ext-1", PayloadHash: "hash-1",
		PlayerID: playerID, WalletID: walletID,
		Kind: wager.KindRefund, Amount: brl("10.00", money.BRL),
		ReferenceExtID: "ext-0", Now: t0,
	})
	if err := tx.AwaitReference(t0.Add(2*time.Second), t0); err != nil {
		t.Fatal(err)
	}
	payload, err := events.WagerTransactionPendingReference(tx)
	if err != nil {
		t.Fatal(err)
	}
	var got events.PendingReferencePayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "PENDING_REFERENCE" {
		t.Errorf("estado errado: %s", got.Status)
	}
	if got.ReferenceExternalTransaction != "ext-0" {
		t.Errorf("referência errada: %s", got.ReferenceExternalTransaction)
	}
	if got.NextAttemptAt == nil || !got.NextAttemptAt.Equal(t0.Add(2*time.Second)) {
		t.Errorf("próxima tentativa errada: %+v", got.NextAttemptAt)
	}
}

func TestWalletBalanceChangedCarregaParAntesDepois(t *testing.T) {
	payload, err := events.WalletBalanceChanged(
		walletID, txID, ledger.Debit,
		brl("25.00", money.BRL), brl("100.00", money.BRL), brl("75.00", money.BRL), 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	var got events.BalanceChangedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Direction != "DEBIT" || got.WalletVersion != 2 {
		t.Errorf("direção/versão erradas: %+v", got)
	}
	if got.BalanceBefore.Amount != "100.00" || got.BalanceAfter.Amount != "75.00" {
		t.Error("par antes/depois errado")
	}
	if got.TransactionID != txID || got.WalletID != walletID {
		t.Error("identidade errada")
	}
}

func TestCanonicalHashDeterministico(t *testing.T) {
	params := wager.ExternalParams{
		ID: "qualquer-uuid-interno", ProviderID: "prov-1", ExternalID: "ext-1",
		IdempotencyKey: "chave-de-outro-transporte", PayloadHash: "hash-anterior",
		PlayerID: playerID, WalletID: walletID,
		Kind: wager.KindBet, Amount: brl("25.00", money.BRL),
		RoundID: "rodada-1", GameID: "jogo-1",
		ReferenceExtID: "", Now: t0,
	}

	h1, err := events.CanonicalHash(params)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := events.CanonicalHash(params)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hash instável: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash devia ser SHA-256 hex, veio %q", h1)
	}

	// Id interno, chave de idempotência e hash anterior não podem entrar no
	// cômputo: o mesmo conteúdo de outro transporte tem de dar o mesmo hash.
	params.ID = "outro-interno"
	params.IdempotencyKey = "outra-chave"
	params.PayloadHash = "outro-hash"
	params.Now = t0.Add(time.Hour)
	h3, err := events.CanonicalHash(params)
	if err != nil {
		t.Fatal(err)
	}
	if h3 != h1 {
		t.Errorf("metadados de transporte entraram no hash: %s vs %s", h3, h1)
	}

	// O conteúdo de negócio muda -> hash muda.
	params.Amount = brl("24.00", money.BRL)
	h4, err := events.CanonicalHash(params)
	if err != nil {
		t.Fatal(err)
	}
	if h4 == h1 {
		t.Error("conteúdo diferente com hash igual")
	}
}

func TestNewEventMontaEnvelopeValido(t *testing.T) {
	evt, err := events.NewEvent("wager_transaction", txID, events.TypeWagerTransactionProcessed, []byte(`{"ok":true}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if evt.EventType() != events.TypeWagerTransactionProcessed {
		t.Errorf("tipo = %s", evt.EventType())
	}
	if evt.Version() != 1 || evt.AggregateID() != txID {
		t.Errorf("envelope errado: %+v", evt)
	}
	if string(evt.Payload()) != `{"ok":true}` {
		t.Errorf("payload = %s", evt.Payload())
	}
}
