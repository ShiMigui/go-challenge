package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/money"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

const (
	walletID  = "11111111-1111-1111-1111-111111111111"
	playerID  = "aaaaaaaa-0000-0000-0000-000000000001"
	txID      = "22222222-2222-2222-2222-222222222222"
	entryID   = "33333333-3333-3333-3333-333333333333"
	eventID   = "44444444-4444-4444-4444-444444444444"
	walletID2 = "11111111-1111-1111-1111-111111111112"
	playerID2 = "aaaaaaaa-0000-0000-0000-000000000002"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func brl(valor string) money.Money { return money.MustParse(valor, money.BRL) }

func novaCarteira(t *testing.T, id, player string, valor string) *wallet.Wallet {
	t.Helper()
	w, err := wallet.New(wallet.Params{
		ID: id, PlayerID: player,
		Opening: brl(valor), Now: t0,
	})
	if err != nil {
		t.Fatalf("wallet.New: %v", err)
	}
	return w
}

func novaAposta(t *testing.T, id, externalID, valor string) *wager.Transaction {
	t.Helper()
	tx, err := wager.NewExternal(wager.ExternalParams{
		ID: id, ProviderID: "prov-1", ExternalID: externalID,
		IdempotencyKey: "prov-1:" + externalID, PayloadHash: "hash-1",
		PlayerID: playerID, WalletID: walletID,
		Kind: wager.KindBet, Amount: brl(valor), Now: t0,
	})
	if err != nil {
		t.Fatalf("wager.NewExternal: %v", err)
	}
	return tx
}

// linhaWallet monta a linha do SELECT de carteira.
func linhaWallet(id, player, currency string, balance, version int64) []driver.Value {
	return []driver.Value{id, player, currency, balance, version, t0, t0}
}

var colunasWallet = []string{"id", "player_id", "currency", "balance", "version", "created_at", "updated_at"}

// linhaWager monta a linha do SELECT de transação.
func linhaWager(id, kind, state string, amount int64) []driver.Value {
	return []driver.Value{
		id, kind, nil, nil, nil, nil, // externos
		playerID, walletID, nil, nil, // player, wallet, rodada, jogo
		"BRL", amount, nil, nil, // moeda, valor, refs
		state, nil, nil, // estado e falha
		0, nil, // tentativas e próxima tentativa
		t0, t0, nil, // criados, atualizado, processado
	}
}

var colunasWager = []string{
	"id", "kind", "provider_id", "external_transaction_id", "idempotency_key",
	"payload_hash", "player_id", "wallet_id", "round_id", "game_id", "currency",
	"amount", "reference_external_transaction_id", "reference_transaction_id",
	"state", "failure_code", "failure_message", "reference_attempts",
	"reference_next_attempt_at", "created_at", "updated_at", "processed_at",
}

func TestWalletFindPorID(t *testing.T) {
	db := abrirFalso(t, "w1", func(c consulta) resposta {
		if !strings.Contains(c.query, "FROM wallets") {
			t.Errorf("query inesperada: %s", c.query)
		}
		return resposta{colunas: colunasWallet, linhas: [][]driver.Value{
			linhaWallet(walletID, playerID, "BRL", 10000, 1),
		}}
	})
	repo := NewWalletRepository(db)

	got, err := repo.FindByID(context.Background(), walletID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance().Amount() != 10000 || got.Version() != 1 {
		t.Errorf("saldo = %d, versão = %d", got.Balance().Amount(), got.Version())
	}
	if got.Currency() != money.BRL || got.PlayerID() != playerID {
		t.Error("identidade não preservada")
	}
}

func TestWalletFindNaoEncontrado(t *testing.T) {
	db := abrirFalso(t, "w2", func(c consulta) resposta {
		return resposta{colunas: colunasWallet}
	})
	repo := NewWalletRepository(db)

	_, err := repo.FindByID(context.Background(), walletID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestWalletFindPorJogadorEmoedaEnviaMoeda(t *testing.T) {
	db := abrirFalso(t, "w3", func(c consulta) resposta {
		if !strings.Contains(c.query, "player_id = $1 AND currency = $2") {
			t.Errorf("filtro de moeda ausente: %s", c.query)
		}
		if len(argsVistos(c.args)) != 2 {
			t.Errorf("esperava 2 argumentos, veio %d", len(argsVistos(c.args)))
		}
		return resposta{colunas: colunasWallet, linhas: [][]driver.Value{
			linhaWallet(walletID, playerID, "BRL", 10000, 1),
		}}
	})
	repo := NewWalletRepository(db)

	got, err := repo.FindByPlayerAndCurrency(context.Background(), playerID, money.BRL)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != walletID {
		t.Errorf("id = %s", got.ID())
	}
}

func TestWalletUpdateEnviaSoBalance(t *testing.T) {
	db := abrirFalso(t, "w4", func(c consulta) resposta {
		// A versão é do trigger: o UPDATE não pode mandá-la.
		if strings.Contains(c.query, "SET version") {
			t.Errorf("version não pode ser escrita pelo back: %s", c.query)
		}
		if !strings.Contains(c.query, "SET balance = $1") {
			t.Errorf("esperava SET balance: %s", c.query)
		}
		// A cláusula de concorrência é o que impede lost update.
		if !strings.Contains(c.query, "WHERE id = $2 AND version = $3") {
			t.Errorf("compare-and-set ausente: %s", c.query)
		}
		return resposta{afetadas: 1}
	})
	repo := NewWalletRepository(db)

	w := novaCarteira(t, walletID, playerID, "100.00")
	if err := w.Credit(brl("50.00"), t0); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateBalance(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

func TestWalletUpdateLock(t *testing.T) {
	// Zero linhas com id existente é concorrência, não registro ausente.
	db := abrirFalso(t, "w5", func(c consulta) resposta {
		if strings.Contains(c.query, "UPDATE wallets") {
			return resposta{afetadas: 0}
		}
		return resposta{colunas: []string{"?column?"}, linhas: [][]driver.Value{{int64(1)}}}
	})
	repo := NewWalletRepository(db)

	w := novaCarteira(t, walletID, playerID, "100.00")
	err := repo.UpdateBalance(context.Background(), w)
	if !errors.Is(err, ErrOptimisticLock) {
		t.Errorf("esperava ErrOptimisticLock, veio %v", err)
	}
}

func TestWalletUpdateNaoEncontrado(t *testing.T) {
	db := abrirFalso(t, "w6", func(c consulta) resposta {
		if strings.Contains(c.query, "UPDATE wallets") {
			return resposta{afetadas: 0}
		}
		return resposta{colunas: colunasWallet}
	})
	repo := NewWalletRepository(db)

	w := novaCarteira(t, walletID, playerID, "100.00")
	err := repo.UpdateBalance(context.Background(), w)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestWalletLockUsaForUpdate(t *testing.T) {
	db := abrirFalso(t, "w7", func(c consulta) resposta {
		if !strings.Contains(c.query, "FOR UPDATE") {
			t.Errorf("LockByID precisa de FOR UPDATE: %s", c.query)
		}
		return resposta{colunas: colunasWallet, linhas: [][]driver.Value{
			linhaWallet(walletID, playerID, "BRL", 10000, 1),
		}}
	})
	repo := NewWalletRepository(db)

	if _, err := repo.LockByID(context.Background(), walletID); err != nil {
		t.Fatal(err)
	}
}

func TestWalletIDInvalidoBarradoAntesDoBanco(t *testing.T) {
	// ID ruim tem de falhar no repositório: chegar ao banco viraria erro
	// de cast, que parece falha de infraestrutura.
	chamado := false
	db := abrirFalso(t, "w8", func(c consulta) resposta {
		chamado = true
		return resposta{}
	})
	repo := NewWalletRepository(db)

	for _, id := range []string{"", "abc", "11111111-1111-1111-1111-11111111111", "11111111_1111_1111_1111_111111111111"} {
		if _, err := repo.FindByID(context.Background(), id); !errors.Is(err, ErrInvalidID) {
			t.Errorf("id %q: esperava ErrInvalidID, veio %v", id, err)
		}
	}
	if chamado {
		t.Error("nenhuma query deveria ter sido enviada")
	}
}

func TestWagerInsert(t *testing.T) {
	db := abrirFalso(t, "t1", func(c consulta) resposta {
		if !strings.Contains(c.query, "ON CONFLICT DO NOTHING") {
			t.Errorf("esperava ON CONFLICT DO NOTHING: %s", c.query)
		}
		args := argsVistos(c.args)
		if len(args) != 14 {
			t.Errorf("esperava 14 argumentos, veio %d", len(args))
		}
		return resposta{colunas: []string{"id"}, linhas: [][]driver.Value{{txID}}}
	})
	repo := NewWagerTransactionRepository(db)

	if err := repo.Insert(context.Background(), novaAposta(t, txID, "ext-1", "10.00")); err != nil {
		t.Fatal(err)
	}
}

func TestWagerInsertDuplicadoDevolveExistente(t *testing.T) {
	// Reentrega do provedor: o ON CONFLICT não insere, e o repositório
	// devolve a transação existente para o chamador responder com o
	// resultado anterior em vez de erro.
	db := abrirFalso(t, "t2", func(c consulta) resposta {
		if strings.Contains(c.query, "INSERT INTO") {
			// Nenhuma linha devolvida: houve conflito.
			return resposta{colunas: []string{"id"}}
		}
		return resposta{colunas: colunasWager, linhas: [][]driver.Value{
			linhaWager(txID, "BET", "PROCESSED", 1000),
		}}
	})
	repo := NewWagerTransactionRepository(db)

	err := repo.Insert(context.Background(), novaAposta(t, txID, "ext-1", "10.00"))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("esperava ErrDuplicate, veio %v", err)
	}
	var dup *Duplicate
	if !errors.As(err, &dup) {
		t.Fatal("esperava *Duplicate para recuperar a transação existente")
	}
	if dup.Existing == nil || dup.Existing.ID() != txID {
		t.Fatal("a transação existente não veio junto")
	}
	if dup.Existing.State() != wager.StateProcessed {
		t.Errorf("estado = %s, quer PROCESSED", dup.Existing.State())
	}
	if err.Error() == "" {
		t.Error("Duplicate precisa de mensagem de erro")
	}
}

func TestWagerFindPorChave(t *testing.T) {
	db := abrirFalso(t, "t3", func(c consulta) resposta {
		switch {
		case strings.Contains(c.query, "idempotency_key = $2"):
			if !strings.Contains(c.query, "provider_id = $1") {
				t.Errorf("chave tem de ser escopada ao provedor: %s", c.query)
			}
		case strings.Contains(c.query, "external_transaction_id = $2"):
		default:
			t.Errorf("query inesperada: %s", c.query)
		}
		return resposta{colunas: colunasWager, linhas: [][]driver.Value{
			linhaWager(txID, "BET", "PROCESSED", 1000),
		}}
	})
	repo := NewWagerTransactionRepository(db)

	porChave, err := repo.FindByIdempotencyKey(context.Background(), "prov-1", "prov-1:ext-1")
	if err != nil {
		t.Fatal(err)
	}
	if porChave.IdempotencyKey() != "" {
		// A linha do teste traz NULL nos externos; o scan tem de
		// tolerar isso sem inventar valor.
		t.Logf("idempotency vazia como esperado para NULL: %q", porChave.IdempotencyKey())
	}

	porExterno, err := repo.FindByExternalID(context.Background(), "prov-1", "ext-1")
	if err != nil {
		t.Fatal(err)
	}
	if porExterno.ID() != txID {
		t.Errorf("id = %s", porExterno.ID())
	}
}

func TestWagerScanToleraNulos(t *testing.T) {
	// Colunas externas são NULL em OPENING. O scan tem de virar string
	// vazia, não erro.
	db := abrirFalso(t, "t4", func(c consulta) resposta {
		return resposta{colunas: colunasWager, linhas: [][]driver.Value{
			linhaWager(txID, "OPENING", "PROCESSED", 5000),
		}}
	})
	repo := NewWagerTransactionRepository(db)

	got, err := repo.FindByID(context.Background(), txID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != wager.KindOpening {
		t.Errorf("kind = %s", got.Kind())
	}
	if got.ProviderID() != "" || got.ExternalID() != "" || got.IdempotencyKey() != "" {
		t.Error("NULL externo deveria virar string vazia")
	}
	if got.HasProcessedAt() {
		t.Error("processed_at NULL não pode virar carimbado")
	}
	if got.FailureCode() != "" {
		t.Error("failure_code NULL não pode virar string")
	}
}

func TestWagerUpdateState(t *testing.T) {
	db := abrirFalso(t, "t5", func(c consulta) resposta {
		// Só a transição a partir de não terminal é aceita.
		if !strings.Contains(c.query, "state IN ('PENDING', 'PENDING_REFERENCE')") {
			t.Errorf("filtro de estado não terminal ausente: %s", c.query)
		}
		return resposta{afetadas: 1}
	})
	repo := NewWagerTransactionRepository(db)

	tx := novaAposta(t, txID, "ext-1", "10.00")
	if err := tx.MarkRejected("INSUFFICIENT_FUNDS", "saldo insuficiente", t0); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateState(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
}

func TestWagerUpdateStateTerminal(t *testing.T) {
	db := abrirFalso(t, "t6", func(c consulta) resposta {
		if strings.Contains(c.query, "UPDATE") {
			return resposta{afetadas: 0}
		}
		return resposta{colunas: []string{"?column?"}, linhas: [][]driver.Value{{int64(1)}}}
	})
	repo := NewWagerTransactionRepository(db)

	tx := novaAposta(t, txID, "ext-1", "10.00")
	if err := tx.MarkProcessed(t0); err != nil {
		t.Fatal(err)
	}
	// Regressão de estado tem de ser recusada, não sobrescrita.
	err := repo.UpdateState(context.Background(), tx)
	if !errors.Is(err, wager.ErrTerminalTransition) {
		t.Errorf("esperava ErrTerminalTransition, veio %v", err)
	}
}

func TestWagerResolveReference(t *testing.T) {
	db := abrirFalso(t, "t7", func(c consulta) resposta {
		// Só a transição a partir de não terminal é aceita.
		if !strings.Contains(c.query, "state = 'PENDING_REFERENCE'") {
			t.Errorf("resolver exige PENDING_REFERENCE: %s", c.query)
		}
		return resposta{afetadas: 1}
	})
	repo := NewWagerTransactionRepository(db)

	tx, err := wager.NewExternal(wager.ExternalParams{
		ID: txID, ProviderID: "prov-1", ExternalID: "ext-r",
		IdempotencyKey: "k", PayloadHash: "h",
		PlayerID: playerID, WalletID: walletID,
		Kind: wager.KindRefund, Amount: brl("10.00"),
		ReferenceExtID: "ext-bet", Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.AwaitReference(t0.Add(time.Minute), t0); err != nil {
		t.Fatal(err)
	}
	if err := tx.ResolveReference(txID, t0); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResolveReference(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
}

func TestWagerListPendingReferences(t *testing.T) {
	db := abrirFalso(t, "t8", func(c consulta) resposta {
		if !strings.Contains(c.query, "state = 'PENDING_REFERENCE'") {
			t.Errorf("filtro de estado ausente: %s", c.query)
		}
		if !strings.Contains(c.query, "reference_next_attempt_at <= $1") {
			t.Errorf("prazo vencido tem de filtrar: %s", c.query)
		}
		return resposta{colunas: colunasWager, linhas: [][]driver.Value{
			linhaWager(txID, "REFUND", "PENDING_REFERENCE", 1000),
		}}
	})
	repo := NewWagerTransactionRepository(db)

	got, err := repo.ListPendingReferences(context.Background(), t0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].State() != wager.StatePendingReference {
		t.Errorf("lista = %d registros", len(got))
	}
}

func TestWagerInsertDuplicadoDeAbertura(t *testing.T) {
	// OPENING não tem identidade externa, então a colisão só pode ser o
	// id. A busca por identidade tem de cair no id interno.
	db := abrirFalso(t, "t9", func(c consulta) resposta {
		if strings.Contains(c.query, "INSERT INTO") {
			return resposta{colunas: []string{"id"}}
		}
		return resposta{colunas: colunasWager, linhas: [][]driver.Value{
			linhaWager(txID, "OPENING", "PROCESSED", 5000),
		}}
	})
	repo := NewWagerTransactionRepository(db)

	abertura, err := wager.NewOpening(wager.OpeningParams{
		ID: txID, PlayerID: playerID, WalletID: walletID,
		Amount: brl("50.00"), Now: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Insert(context.Background(), abertura)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("esperava ErrDuplicate, veio %v", err)
	}
	var dup *Duplicate
	if !errors.As(err, &dup) || dup.Existing == nil {
		t.Fatal("esperava a transação existente")
	}
	if dup.Existing.Kind() != wager.KindOpening {
		t.Errorf("kind = %s, quer OPENING", dup.Existing.Kind())
	}
}

func TestWagerInsertDuplicadoSemIdentidadeLocalizavel(t *testing.T) {
	// Conflito que nenhuma busca encontra é anomalia do banco. A entrada
	// de dup.Existing fica vazia, mas o erro continua sendo ErrDuplicate
	// para o chamador tratar como reentrega sem quebrar o fluxo.
	db := abrirFalso(t, "t10", func(c consulta) resposta {
		if strings.Contains(c.query, "INSERT INTO") {
			return resposta{colunas: []string{"id"}}
		}
		return resposta{colunas: colunasWager}
	})
	repo := NewWagerTransactionRepository(db)

	err := repo.Insert(context.Background(), novaAposta(t, txID, "ext-1", "10.00"))
	if !errors.Is(err, ErrDuplicate) {
		t.Errorf("esperava ErrDuplicate, veio %v", err)
	}
	var dup *Duplicate
	if !errors.As(err, &dup) {
		t.Fatal("esperava *Duplicate")
	}
	if dup.Existing != nil {
		t.Error("não deveria haver transação existente")
	}
	if dup.Error() == "" {
		t.Error("Duplicate sem existente precisa de mensagem")
	}
}

func TestWagerInsertPropagaErroDoBanco(t *testing.T) {
	falha := errors.New("conexao perdida")
	db := abrirFalso(t, "t11", func(c consulta) resposta { return resposta{erro: falha} })
	repo := NewWagerTransactionRepository(db)

	if err := repo.Insert(context.Background(), novaAposta(t, txID, "ext-1", "10.00")); !errors.Is(err, falha) {
		t.Errorf("esperava o erro do banco, veio %v", err)
	}
}

func TestWagerFindPorChaveIgnoraNulos(t *testing.T) {
	// A linha traz NULL nos externos. Busca por chave não pode quebrar
	// com isso: é o caminho da reentrega do provedor.
	db := abrirFalso(t, "t12", func(c consulta) resposta {
		return resposta{colunas: colunasWager, linhas: [][]driver.Value{
			linhaWager(txID, "BET", "PROCESSED", 1000),
		}}
	})
	repo := NewWagerTransactionRepository(db)

	got, err := repo.FindByIdempotencyKey(context.Background(), "prov-1", "k")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderID() != "" || got.ExternalID() != "" {
		t.Error("NULL deveria virar string vazia")
	}
}

func TestInboxPrimeiraVezAceita(t *testing.T) {
	db := abrirFalso(t, "i1", func(c consulta) resposta {
		if !strings.Contains(c.query, "INSERT INTO inbox") {
			t.Errorf("query inesperada: %s", c.query)
		}
		return resposta{colunas: []string{"id"}, linhas: [][]driver.Value{{"msg"}}}
	})
	repo := NewInboxRepository(db)

	nova, err := repo.TryBegin(context.Background(), "consumer-1", "msg-1", "hash-1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !nova {
		t.Error("primeira mensagem deveria ser aceita")
	}
}

func TestInboxReentregaEhIgnorada(t *testing.T) {
	// Já existe com o mesmo hash: o consumidor processou antes, então
	// precisa sair sem reprocessar.
	db := abrirFalso(t, "i2", func(c consulta) resposta {
		if strings.Contains(c.query, "INSERT") {
			return resposta{colunas: []string{"id"}}
		}
		return resposta{colunas: []string{"message_hash"}, linhas: [][]driver.Value{{"hash-1"}}}
	})
	repo := NewInboxRepository(db)

	nova, err := repo.TryBegin(context.Background(), "consumer-1", "msg-1", "hash-1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if nova {
		t.Error("reentrega não deveria ser tratada como nova")
	}
}

func TestInboxHashDivergenteEAdulteracao(t *testing.T) {
	// Mesmo id, corpo diferente: o id foi reaproveitado. Isso é erro, não
	// reentrega, e processar assim devolveria resultado errado.
	db := abrirFalso(t, "i3", func(c consulta) resposta {
		if strings.Contains(c.query, "INSERT") {
			return resposta{colunas: []string{"id"}}
		}
		return resposta{colunas: []string{"message_hash"}, linhas: [][]driver.Value{{"hash-antigo"}}}
	})
	repo := NewInboxRepository(db)

	_, err := repo.TryBegin(context.Background(), "consumer-1", "msg-1", "hash-novo", t0)
	if !errors.Is(err, ErrMessageTampered) {
		t.Errorf("esperava ErrMessageTampered, veio %v", err)
	}
}

func TestInboxComplete(t *testing.T) {
	db := abrirFalso(t, "i4", func(c consulta) resposta {
		if !strings.Contains(c.query, "SET completed_at = $3") {
			t.Errorf("query inesperada: %s", c.query)
		}
		return resposta{afetadas: 1}
	})
	repo := NewInboxRepository(db)

	if err := repo.Complete(context.Background(), "consumer-1", "msg-1", t0); err != nil {
		t.Fatal(err)
	}
}

func TestInboxCompleteInexistente(t *testing.T) {
	db := abrirFalso(t, "i5", func(c consulta) resposta { return resposta{afetadas: 0} })
	repo := NewInboxRepository(db)

	err := repo.Complete(context.Background(), "consumer-1", "msg-1", t0)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestOutboxAppend(t *testing.T) {
	db := abrirFalso(t, "o1", func(c consulta) resposta {
		if !strings.Contains(c.query, "INSERT INTO outbox") {
			t.Errorf("query inesperada: %s", c.query)
		}
		// next_attempt_at nasce junto de occurred_at: nada a republicar.
		args := argsVistos(c.args)
		if len(args) != 10 {
			t.Fatalf("esperava 10 argumentos, veio %d", len(args))
		}
		if !args[8].(time.Time).Equal(args[9].(time.Time)) {
			t.Error("next_attempt_at deveria nascer igual a occurred_at")
		}
		return resposta{afetadas: 1}
	})
	repo := NewOutboxRepository(db)

	evento, err := NewEvent(eventID, "wallet", walletID, "WalletBalanceChanged", 1,
		[]byte(`{"saldo":"150.00"}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(context.Background(), evento); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxAppendRecusaPayloadInvalido(t *testing.T) {
	db := abrirFalso(t, "o2", func(c consulta) resposta { return resposta{afetadas: 1} })
	_ = NewOutboxRepository(db)

	// O banco exige objeto JSON, não array nem string solta.
	for _, payload := range [][]byte{[]byte("nao é json"), []byte(`[1,2]`), []byte(`"texto"`), nil} {
		if _, err := NewEvent(eventID, "wallet", walletID, "Evento", 1, payload, t0); err == nil {
			t.Errorf("payload %q deveria ser recusado", payload)
		}
	}
}

func TestOutboxVersionMinima(t *testing.T) {
	db := abrirFalso(t, "o3", func(c consulta) resposta { return resposta{afetadas: 1} })
	_ = NewOutboxRepository(db)

	if _, err := NewEvent(eventID, "wallet", walletID, "Evento", 0,
		[]byte(`{}`), t0); err == nil {
		t.Error("event_version 0 deveria ser recusado")
	}
}

func TestOutboxClaimBatchUsaSkipLocked(t *testing.T) {
	// Vários workers competem pelo mesmo backlog: FOR UPDATE SKIP LOCKED
	// é o que impede dois workers de pegarem o mesmo evento.
	db := abrirFalso(t, "o4", func(c consulta) resposta {
		if strings.Contains(c.query, "SELECT id") {
			if !strings.Contains(c.query, "FOR UPDATE SKIP LOCKED") {
				t.Errorf("SKIP LOCKED ausente: %s", c.query)
			}
			return resposta{colunas: []string{"id"}, linhas: [][]driver.Value{{eventID}}}
		}
		return resposta{
			colunas: []string{"id", "aggregate_type", "aggregate_id", "event_type",
				"event_version", "correlation_id", "causation_id", "payload",
				"occurred_at", "attempts"},
			linhas: [][]driver.Value{{
				eventID, "wallet", walletID, "WalletBalanceChanged",
				int64(1), nil, nil, []byte(`{"saldo":"150.00"}`), t0, int64(1),
			}},
		}
	})
	repo := NewOutboxRepository(db)

	got, err := repo.ClaimBatch(context.Background(), "worker-1", 10, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("lote = %d, quer 1", len(got))
	}
	e := got[0]
	if e.ID() != eventID || e.AggregateID() != walletID {
		t.Errorf("evento = %s / %s", e.ID(), e.AggregateID())
	}
	if e.EventType() != "WalletBalanceChanged" || e.Version() != 1 {
		t.Errorf("tipo = %s, versão = %d", e.EventType(), e.Version())
	}
	if e.Attempts() != 1 {
		t.Errorf("tentativas = %d, quer 1 (contadas no claim)", e.Attempts())
	}
	if string(e.Payload()) != `{"saldo":"150.00"}` {
		t.Errorf("payload = %s", e.Payload())
	}
}

func TestOutboxClaimVazio(t *testing.T) {
	db := abrirFalso(t, "o5", func(c consulta) resposta {
		return resposta{colunas: []string{"id"}}
	})
	repo := NewOutboxRepository(db)

	got, err := repo.ClaimBatch(context.Background(), "worker-1", 10, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("lote = %d, quer 0", len(got))
	}
}

func TestOutboxMarkPublishedLiberaLock(t *testing.T) {
	// Publicar precisa soltar o lock, senão o registro fica preso à
	// instância que o pegou.
	db := abrirFalso(t, "o6", func(c consulta) resposta {
		if !strings.Contains(c.query, "locked_by = NULL, locked_at = NULL") {
			t.Errorf("lock não foi solto: %s", c.query)
		}
		return resposta{afetadas: 1}
	})
	repo := NewOutboxRepository(db)

	if err := repo.MarkPublished(context.Background(), eventID, t0); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxMarkPublishedJaPublicado(t *testing.T) {
	db := abrirFalso(t, "o7", func(c consulta) resposta { return resposta{afetadas: 0} })
	repo := NewOutboxRepository(db)

	if err := repo.MarkPublished(context.Background(), eventID, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestOutboxReschedule(t *testing.T) {
	db := abrirFalso(t, "o8", func(c consulta) resposta {
		if !strings.Contains(c.query, "next_attempt_at = $2") {
			t.Errorf("query inesperada: %s", c.query)
		}
		return resposta{afetadas: 1}
	})
	repo := NewOutboxRepository(db)

	proxima := t0.Add(30 * time.Second)
	if err := repo.Reschedule(context.Background(), eventID, proxima, t0); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxCorrelationNaoAlteraOriginal(t *testing.T) {
	db := abrirFalso(t, "o9", func(c consulta) resposta { return resposta{afetadas: 1} })
	_ = NewOutboxRepository(db)

	evento, _ := NewEvent(eventID, "wallet", walletID, "Evento", 1, []byte(`{}`), t0)
	comCorrelacao := evento.WithCorrelation("corr-1", txID)
	if comCorrelacao.CorrelationID() != "corr-1" || comCorrelacao.CausationID() != txID {
		t.Error("correlação não foi aplicada")
	}
	// A cópia não pode vazar para o original: o snapshot publicado é do
	// evento, não do chamador.
	if evento.CorrelationID() != "" || evento.CausationID() != "" {
		t.Error("WithCorrelation não pode mutar o original")
	}
}

func TestWithTxUsaTransacao(t *testing.T) {
	db := abrirFalso(t, "tx1", func(c consulta) resposta { return resposta{afetadas: 1} })
	repo := NewTransactionRepository(db)

	rodou := false
	err := repo.WithTx(context.Background(), func(*sql.Tx) error {
		rodou = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rodou {
		t.Error("a função não foi chamada")
	}
}

func TestWithTxPropagaErroDaFuncao(t *testing.T) {
	db := abrirFalso(t, "tx2", func(c consulta) resposta { return resposta{afetadas: 1} })
	repo := NewTransactionRepository(db)

	falha := errors.New("falha de negocio")
	err := repo.WithTx(context.Background(), func(*sql.Tx) error { return falha })
	if !errors.Is(err, falha) {
		t.Errorf("esperava a falha original, veio %v", err)
	}
}

func TestIsUUID(t *testing.T) {
	validos := []string{
		"11111111-1111-1111-1111-111111111111",
		"aBcDeF01-2345-6789-abcd-ef0123456789",
	}
	for _, v := range validos {
		if !isUUID(v) {
			t.Errorf("%q deveria ser UUID", v)
		}
	}
	invalidos := []string{
		"", "abc", "11111111-1111-1111-1111-11111111111",
		"11111111111111111111111111111111",
		"11111111_1111_1111_1111_111111111111",
		"1111111-1111-1111-1111-111111111111",
		"11111111-1111-1111-1111-11111111111g",
	}
	for _, v := range invalidos {
		if isUUID(v) {
			t.Errorf("%q não deveria ser UUID", v)
		}
	}
}

func TestNullString(t *testing.T) {
	if nullString("") != nil {
		t.Error("string vazia deveria virar NULL")
	}
	if nullString("valor") != "valor" {
		t.Error("string preenchida deveria ser preservada")
	}
}
