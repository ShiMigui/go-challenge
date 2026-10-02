package persistence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/event"
	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
)

var colunasLedger = []string{
	"id", "wallet_id", "transaction_id", "direction", "amount", "currency",
	"balance_before", "balance_after", "created_at",
}

func linhaLedger(id, direction string, amount, antes, depois int64) []driver.Value {
	return []driver.Value{
		id, walletID, txID, direction, amount, "BRL", antes, depois, t0,
	}
}

func novoLancamento(t *testing.T, direction ledger.Direction, valor, antes, depois string) *ledger.Entry {
	t.Helper()
	e, err := ledger.New(ledger.Params{
		ID: entryID, WalletID: walletID, TransactionID: txID,
		Direction:     direction,
		Amount:        brl(valor),
		BalanceBefore: brl(antes), BalanceAfter: brl(depois),
		Now: t0,
	})
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	return e
}

func TestLedgerAppend(t *testing.T) {
	db := abrirFalso(t, "l1", func(c consulta) resposta {
		if !strings.Contains(c.query, "INSERT INTO wallet_ledger_entries") {
			t.Errorf("query inesperada: %s", c.query)
		}
		return resposta{colunas: []string{"id"}, linhas: [][]driver.Value{{entryID}}}
	})
	repo := NewLedgerRepository(db)

	if err := repo.Append(context.Background(),
		novoLancamento(t, ledger.Credit, "50.00", "100.00", "150.00")); err != nil {
		t.Fatal(err)
	}
}

func TestLedgerAppendDuplicadoNaoFalha(t *testing.T) {
	// Reentrega at-least-once tenta de novo; o Append precisa sair
	// quieto em vez de derrubar o processamento.
	db := abrirFalso(t, "l2", func(c consulta) resposta {
		return resposta{colunas: []string{"id"}}
	})
	repo := NewLedgerRepository(db)

	err := repo.Append(context.Background(),
		novoLancamento(t, ledger.Credit, "50.00", "100.00", "150.00"))
	if !errors.Is(err, wager.ErrDuplicate) {
		t.Errorf("esperava ErrDuplicate, veio %v", err)
	}
}

func TestLedgerAppendRecusaIDInvalido(t *testing.T) {
	db := abrirFalso(t, "l3", func(c consulta) resposta { return resposta{afetadas: 1} })
	repo := NewLedgerRepository(db)

	e, _ := ledger.New(ledger.Params{
		ID: "nao-e-uuid", WalletID: walletID, TransactionID: txID,
		Direction: ledger.Credit, Amount: brl("1.00"),
		BalanceBefore: brl("0.00"), BalanceAfter: brl("1.00"), Now: t0,
	})
	if err := repo.Append(context.Background(), e); !errors.Is(err, identifier.ErrInvalidID) {
		t.Errorf("esperava ErrInvalidID, veio %v", err)
	}
}

func TestLedgerFindPorTransaction(t *testing.T) {
	db := abrirFalso(t, "l4", func(c consulta) resposta {
		if !strings.Contains(c.query, "WHERE transaction_id = $1") {
			t.Errorf("query inesperada: %s", c.query)
		}
		return resposta{colunas: colunasLedger, linhas: [][]driver.Value{
			linhaLedger(entryID, "CREDIT", 5000, 10000, 15000),
		}}
	})
	repo := NewLedgerRepository(db)

	got, err := repo.FindByTransaction(context.Background(), txID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Direction() != ledger.Credit {
		t.Errorf("direção = %s", got.Direction())
	}
	if got.Amount().Amount() != 5000 {
		t.Errorf("valor = %d", got.Amount().Amount())
	}
	if got.BalanceBefore().Amount() != 10000 || got.BalanceAfter().Amount() != 15000 {
		t.Error("saldos não preservados")
	}
}

func TestLedgerFindNaoEncontrado(t *testing.T) {
	db := abrirFalso(t, "l5", func(c consulta) resposta {
		return resposta{colunas: colunasLedger}
	})
	repo := NewLedgerRepository(db)

	if _, err := repo.FindByTransaction(context.Background(), txID); !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestLedgerListPorWallet(t *testing.T) {
	db := abrirFalso(t, "l6", func(c consulta) resposta {
		// Extrato vai do mais novo para o mais antigo.
		if !strings.Contains(c.query, "ORDER BY created_at DESC") {
			t.Errorf("ordenação do extrato ausente: %s", c.query)
		}
		return resposta{colunas: colunasLedger, linhas: [][]driver.Value{
			linhaLedger(entryID, "CREDIT", 5000, 10000, 15000),
			linhaLedger(entryID, "DEBIT", 10000, 10000, 0),
		}}
	})
	repo := NewLedgerRepository(db)

	got, err := repo.ListByWallet(context.Background(), walletID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("extrato = %d lançamentos, quer 2", len(got))
	}
	if got[0].Direction() != ledger.Credit || got[1].Direction() != ledger.Debit {
		t.Error("ordem do extrato não preservada")
	}
}

func TestWalletInsert(t *testing.T) {
	db := abrirFalso(t, "w10", func(c consulta) resposta {
		if !strings.Contains(c.query, "INSERT INTO wallets") {
			t.Errorf("query inesperada: %s", c.query)
		}
		args := argsVistos(c.args)
		if len(args) != 4 {
			t.Errorf("esperava 4 argumentos, veio %d", len(args))
		}
		// O saldo vai como unidade mínima, não como string decimal.
		if args[3].(int64) != 10000 {
			t.Errorf("saldo enviado = %v, quer 10000", args[3])
		}
		return resposta{afetadas: 1}
	})
	repo := NewWalletRepository(db)

	if err := repo.Insert(context.Background(), novaCarteira(t, walletID, playerID, "100.00")); err != nil {
		t.Fatal(err)
	}
}

func TestWalletInsertRecusaIDInvalido(t *testing.T) {
	db := abrirFalso(t, "w11", func(c consulta) resposta { return resposta{afetadas: 1} })
	repo := NewWalletRepository(db)

	w, _ := wallet.New(wallet.Params{
		ID: "invalido", PlayerID: playerID,
		Opening: brl("0.00"), Now: t0,
	})
	if err := repo.Insert(context.Background(), w); !errors.Is(err, identifier.ErrInvalidID) {
		t.Errorf("esperava ErrInvalidID, veio %v", err)
	}
}

func TestInboxIsCompleted(t *testing.T) {
	db := abrirFalso(t, "i10", func(c consulta) resposta {
		return resposta{colunas: []string{"completed_at"}, linhas: [][]driver.Value{{t0}}}
	})
	repo := NewInboxRepository(db)

	concluida, err := repo.IsCompleted(context.Background(), "consumer-1", "msg-1")
	if err != nil {
		t.Fatal(err)
	}
	if !concluida {
		t.Error("mensagem com completed_at deveria estar concluída")
	}
}

func TestInboxIsCompletedPendente(t *testing.T) {
	db := abrirFalso(t, "i11", func(c consulta) resposta {
		return resposta{colunas: []string{"completed_at"}, linhas: [][]driver.Value{{nil}}}
	})
	repo := NewInboxRepository(db)

	concluida, err := repo.IsCompleted(context.Background(), "consumer-1", "msg-1")
	if err != nil {
		t.Fatal(err)
	}
	if concluida {
		t.Error("completed_at NULL significa que ainda não foi concluída")
	}
}

func TestInboxIsCompletedInexistente(t *testing.T) {
	db := abrirFalso(t, "i12", func(c consulta) resposta {
		return resposta{colunas: []string{"completed_at"}}
	})
	repo := NewInboxRepository(db)

	if _, err := repo.IsCompleted(context.Background(), "consumer-1", "msg-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestInboxCountAttempts(t *testing.T) {
	db := abrirFalso(t, "i13", func(c consulta) resposta {
		return resposta{colunas: []string{"attempts"}, linhas: [][]driver.Value{{int64(3)}}}
	})
	repo := NewInboxRepository(db)

	got, err := repo.CountAttempts(context.Background(), "consumer-1", "msg-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("tentativas = %d, quer 3", got)
	}
}

func TestInboxCountAttemptsInexistente(t *testing.T) {
	db := abrirFalso(t, "i14", func(c consulta) resposta {
		return resposta{colunas: []string{"attempts"}}
	})
	repo := NewInboxRepository(db)

	if _, err := repo.CountAttempts(context.Background(), "consumer-1", "msg-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestOutboxGetters(t *testing.T) {
	e, err := event.NewEvent(eventID, "wallet", walletID, "WalletBalanceChanged", 2,
		[]byte(`{"saldo":"150.00"}`), t0)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID() != eventID {
		t.Errorf("id = %s", e.ID())
	}
	if e.AggregateType() != "wallet" {
		t.Errorf("tipo do agregado = %s", e.AggregateType())
	}
	if !e.OccurredAt().Equal(t0) {
		t.Errorf("ocorrido em = %v, quer %v", e.OccurredAt(), t0)
	}
	if e.Attempts() != 0 {
		t.Errorf("tentativas iniciais = %d, quer 0", e.Attempts())
	}
}

func TestNewEventRecusaIDInvalido(t *testing.T) {
	db := abrirFalso(t, "o10", func(c consulta) resposta { return resposta{afetadas: 1} })
	_ = NewOutboxRepository(db)

	if _, err := event.NewEvent("nao-e-uuid", "wallet", walletID, "Evento", 1, []byte(`{}`), t0); !errors.Is(err, identifier.ErrInvalidID) {
		t.Errorf("event_id inválido deveria falhar")
	}
	if _, err := event.NewEvent(eventID, "wallet", "nao-e-uuid", "Evento", 1, []byte(`{}`), t0); !errors.Is(err, identifier.ErrInvalidID) {
		t.Errorf("aggregate_id inválido deveria falhar")
	}
}

func nullTime() sql.NullTime { return sql.NullTime{} }

func timeValido() sql.NullTime { return sql.NullTime{Time: t0, Valid: true} }

func TestTimeOrZero(t *testing.T) {
	// Helper de scan: NULL tem de virar instante zero, e um valor
	// presente precisa preservar o horário que veio do banco.
	if !timeOrZero(nullTime()).IsZero() {
		t.Error("NULL deveria virar instante zero")
	}
	if timeOrZero(timeValido()).IsZero() {
		t.Error("valor presente não pode virar instante zero")
	}
	if !timeOrZero(timeValido()).Equal(t0) {
		t.Error("valor presente precisa preservar o horário do banco")
	}
}

func TestRepositorioRejeitaErroDoBanco(t *testing.T) {
	// Erro do driver tem de subir cru: traduzir esconderia falha de
	// infraestrutura atrás de mensagem de regra de negócio.
	falha := errors.New("conexao perdida")
	db := abrirFalso(t, "e1", func(c consulta) resposta { return resposta{erro: falha} })
	repo := NewWalletRepository(db)

	if _, err := repo.FindByID(context.Background(), walletID); !errors.Is(err, falha) {
		t.Errorf("esperava o erro do banco, veio %v", err)
	}
}

func TestLockExpiracaoEhCurto(t *testing.T) {
	// Um lock longo prenderia o registro para sempre se o worker morresse.
	if lockExpiracao > 5*time.Minute {
		t.Errorf("expiração de lock = %v, alto demais para recuperar worker órfão", lockExpiracao)
	}
}
