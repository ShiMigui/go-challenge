package wager

import (
	"errors"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
)

var agora = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func abertura(t *testing.T) *Transaction {
	t.Helper()
	tx, err := NewOpening(OpeningParams{
		ID: "tx-open", PlayerID: "p1", WalletID: "w1",
		Amount: money.MustParse("100.00", money.BRL), Now: agora,
	})
	if err != nil {
		t.Fatalf("NewOpening: %v", err)
	}
	return tx
}

func externa(t *testing.T, p ExternalParams) *Transaction {
	t.Helper()
	if p.ID == "" {
		p.ID = "tx-ext"
	}
	if p.ProviderID == "" {
		p.ProviderID = "prov-1"
	}
	if p.ExternalID == "" {
		p.ExternalID = "ext-1"
	}
	if p.IdempotencyKey == "" {
		p.IdempotencyKey = p.ProviderID + ":" + p.ExternalID
	}
	if p.PayloadHash == "" {
		p.PayloadHash = "hash-1"
	}
	if p.PlayerID == "" {
		p.PlayerID = "p1"
	}
	if p.WalletID == "" {
		p.WalletID = "w1"
	}
	if p.Kind == "" {
		p.Kind = KindBet
	}
	if p.Amount.Currency() == "" {
		p.Amount = money.MustParse("10.00", money.BRL)
	}
	if p.Now.IsZero() {
		p.Now = agora
	}
	if p.ReferenceExtID == "" && p.Kind.RequiresReference() {
		p.ReferenceExtID = "ext-0"
	}
	tx, err := NewExternal(p)
	if err != nil {
		t.Fatalf("NewExternal: %v", err)
	}
	return tx
}

// externoErr monta uma operação externa devolvendo o erro, para os casos
// em que a construção é recusada.
func externoErr(t *testing.T, p ExternalParams) error {
	t.Helper()
	if p.ID == "" {
		p.ID = "tx-ext"
	}
	if p.ProviderID == "" {
		p.ProviderID = "prov-1"
	}
	if p.ExternalID == "" {
		p.ExternalID = "ext-1"
	}
	if p.IdempotencyKey == "" {
		p.IdempotencyKey = p.ProviderID + ":" + p.ExternalID
	}
	if p.PayloadHash == "" {
		p.PayloadHash = "hash-1"
	}
	if p.PlayerID == "" {
		p.PlayerID = "p1"
	}
	if p.WalletID == "" {
		p.WalletID = "w1"
	}
	if p.Kind == "" {
		p.Kind = KindBet
	}
	if p.Amount.Currency() == "" {
		p.Amount = money.MustParse("10.00", money.BRL)
	}
	if p.Now.IsZero() {
		p.Now = agora
	}
	_, err := NewExternal(p)
	return err
}

func TestNovaTransacaoComecaPending(t *testing.T) {
	tx := externa(t, ExternalParams{Kind: KindBet})
	if tx.State() != StatePending {
		t.Errorf("estado = %s, quer PENDING", tx.State())
	}
	if !tx.IsPending() || tx.IsPending() == false {
		t.Error("IsPending deveria ser true em PENDING")
	}
	if tx.State().IsTerminal() {
		t.Error("PENDING não é terminal")
	}
}

func TestAberturaNaoTemCamposExternos(t *testing.T) {
	tx := abertura(t)
	if tx.ProviderID() != "" || tx.ExternalID() != "" ||
		tx.IdempotencyKey() != "" || tx.PayloadHash() != "" ||
		tx.RoundID() != "" || tx.GameID() != "" || tx.ReferenceExternalID() != "" {
		t.Error("abertura não deveria carregar campo externo")
	}
	if tx.Kind() != KindOpening {
		t.Errorf("kind = %s", tx.Kind())
	}
}

func TestAberturaNaoVeioDeFora(t *testing.T) {
	// Chegar como operação externa com OPENING significa que veio de fora:
	// tem que ser recusado.
	_, err := NewExternal(ExternalParams{
		ID: "tx1", ProviderID: "prov", ExternalID: "ext",
		IdempotencyKey: "k", PayloadHash: "h",
		PlayerID: "p", WalletID: "w",
		Kind: KindOpening, Amount: money.MustParse("10.00", money.BRL), Now: agora,
	})
	if !errors.Is(err, ErrExternalNotAllowed) {
		t.Errorf("esperava ErrExternalNotAllowed, veio %v", err)
	}
}

func TestValidacoesOperacaoExterna(t *testing.T) {
	base := func() ExternalParams {
		return ExternalParams{
			ID: "tx1", ProviderID: "prov", ExternalID: "ext",
			IdempotencyKey: "k", PayloadHash: "h",
			PlayerID: "p", WalletID: "w",
			Kind: KindBet, Amount: money.MustParse("10.00", money.BRL), Now: agora,
		}
	}
	cases := []struct {
		nome  string
		mutar func(*ExternalParams)
		want  error
	}{
		{"sem id", func(p *ExternalParams) { p.ID = "" }, identifier.ErrInvalidTransactionID},
		{"kind desconhecido", func(p *ExternalParams) { p.Kind = "CASHBACK" }, ErrInvalidKind},
		{"sem provider", func(p *ExternalParams) { p.ProviderID = "" }, ErrInvalidProviderID},
		{"sem id externo", func(p *ExternalParams) { p.ExternalID = "" }, ErrInvalidExternalID},
		{"sem chave", func(p *ExternalParams) { p.IdempotencyKey = "" }, ErrInvalidIdempotencyKey},
		{"sem hash", func(p *ExternalParams) { p.PayloadHash = "" }, ErrInvalidPayloadHash},
		{"sem player", func(p *ExternalParams) { p.PlayerID = "" }, identifier.ErrInvalidPlayerID},
		{"sem wallet", func(p *ExternalParams) { p.WalletID = "" }, identifier.ErrInvalidWalletID},
		{"BET com valor zero", func(p *ExternalParams) { p.Amount = money.Zero(money.BRL) }, ErrInvalidAmountForKind},
		{"BET com valor negativo", func(p *ExternalParams) {
			p.Amount = money.MustNew(-1000, money.BRL)
		}, ErrInvalidAmountForKind},
		{"LOSS com valor", func(p *ExternalParams) {
			p.Kind = KindLoss
			p.Amount = money.MustParse("10.00", money.BRL)
		}, ErrInvalidAmountForKind},
		{"BET com referencia", func(p *ExternalParams) { p.ReferenceExtID = "ext-0" }, ErrForbiddenReference},
		{"LOSS com referencia", func(p *ExternalParams) {
			p.Kind = KindLoss
			p.Amount = money.Zero(money.BRL)
			p.ReferenceExtID = "ext-0"
		}, ErrForbiddenReference},
		{"REFUND sem referencia", func(p *ExternalParams) { p.Kind = KindRefund }, ErrMissingReference},
		{"ROLLBACK sem referencia", func(p *ExternalParams) { p.Kind = KindRollback }, ErrMissingReference},
	}
	for _, c := range cases {
		p := base()
		c.mutar(&p)
		_, err := NewExternal(p)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: esperava %v, veio %v", c.nome, c.want, err)
		}
	}
}

func TestValoresAceitosPorTipo(t *testing.T) {
	cases := []struct {
		kind   Kind
		valor  string
		refer  string
		aceita bool
	}{
		{KindBet, "10.00", "", true},
		{KindBet, "0.00", "", false},
		{KindWin, "10.00", "", true},
		{KindWin, "10.00", "ext-bet", true},
		{KindLoss, "0.00", "", true},
		{KindLoss, "0.01", "", false},
		{KindRefund, "10.00", "ext-bet", true},
		{KindRollback, "10.00", "ext-bet", true},
	}
	for _, c := range cases {
		err := externoErr(t, ExternalParams{
			Kind: c.kind, Amount: money.MustParse(c.valor, money.BRL),
			ReferenceExtID: c.refer,
		})
		if c.aceita && err != nil {
			t.Errorf("%s %s ref=%q: esperava aceitar, veio %v", c.kind, c.valor, c.refer, err)
		}
		if !c.aceita && err == nil {
			t.Errorf("%s %s ref=%q: esperava recusar", c.kind, c.valor, c.refer)
		}
	}
}

func TestMaquinaDeEstados(t *testing.T) {
	tx := externa(t, ExternalParams{Kind: KindBet})
	if err := tx.MarkProcessed(agora); err != nil {
		t.Fatal(err)
	}
	if tx.State() != StateProcessed || !tx.State().IsTerminal() {
		t.Errorf("estado = %s", tx.State())
	}
	if !tx.HasProcessedAt() || !tx.ProcessedAt().Equal(agora) {
		t.Error("processedAt não carimbado")
	}
	// Terminal não aceita mais nada.
	if err := tx.MarkProcessed(agora); !errors.Is(err, ErrTerminalTransition) {
		t.Errorf("reprocessar: esperava ErrTerminalTransition, veio %v", err)
	}
	if err := tx.MarkRejected("R", "r", agora); !errors.Is(err, ErrTerminalTransition) {
		t.Errorf("rejeitar processed: esperava ErrTerminalTransition, veio %v", err)
	}
	if err := tx.MarkFailed("F", "f", agora); !errors.Is(err, ErrTerminalTransition) {
		t.Errorf("falhar processed: esperava ErrTerminalTransition, veio %v", err)
	}
}

func TestRejeicaoEFalhaExigemCodigo(t *testing.T) {
	for _, aplicar := range []struct {
		nome string
		fn   func(*Transaction, string, string) error
	}{
		{"rejected", func(tx *Transaction, c, m string) error { return tx.MarkRejected(c, m, agora) }},
		{"failed", func(tx *Transaction, c, m string) error { return tx.MarkFailed(c, m, agora) }},
	} {
		tx := externa(t, ExternalParams{Kind: KindBet})
		if err := aplicar.fn(tx, "", "msg"); !errors.Is(err, ErrFailureCodeRequired) {
			t.Errorf("%s sem código: esperava ErrFailureCodeRequired, veio %v", aplicar.nome, err)
		}
		if err := aplicar.fn(tx, "CODE", "msg"); err != nil {
			t.Fatalf("%s: %v", aplicar.nome, err)
		}
		if tx.FailureCode() != "CODE" || tx.FailureMessage() != "msg" {
			t.Errorf("%s: falha não registrada", aplicar.nome)
		}
		if tx.State().IsTerminal() == false {
			t.Errorf("%s deveria ser terminal", aplicar.nome)
		}
	}
}

func TestRejeitadoEFalhaoSaoDistintos(t *testing.T) {
	// A distinção importa: REJECTED é resposta correta a entrada inválida,
	// FAILED é problema nosso registrado para auditoria.
	rej := externa(t, ExternalParams{Kind: KindBet, ExternalID: "r"})
	fal := externa(t, ExternalParams{Kind: KindBet, ExternalID: "f"})
	if err := rej.MarkRejected("INSUFFICIENT_FUNDS", "saldo", agora); err != nil {
		t.Fatal(err)
	}
	if err := fal.MarkFailed("DB_TIMEOUT", "timeout", agora); err != nil {
		t.Fatal(err)
	}
	if rej.State() != StateRejected || fal.State() != StateFailed {
		t.Errorf("estados = %s e %s", rej.State(), fal.State())
	}
}

func TestPendenteDeReferencia(t *testing.T) {
	tx := externa(t, ExternalParams{Kind: KindRefund, ReferenceExtID: "ext-bet"})
	proxima := agora.Add(time.Minute)
	if err := tx.AwaitReference(proxima, agora); err != nil {
		t.Fatal(err)
	}
	if tx.State() != StatePendingReference {
		t.Errorf("estado = %s", tx.State())
	}
	if !tx.ReferenceNextAttempt().Equal(proxima) {
		t.Error("próxima tentativa não registrada")
	}
	// Quem estava em espera pode concluir quando a referência chega.
	if err := tx.ResolveReference("tx-bet", agora); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkProcessed(agora); err != nil {
		t.Fatal(err)
	}
	if tx.State() != StateProcessed || tx.ReferenceID() != "tx-bet" {
		t.Errorf("estado = %s, referência = %s", tx.State(), tx.ReferenceID())
	}
}

func TestNaoReversaoNaoAguardaReferencia(t *testing.T) {
	// BET, WIN e LOSS não têm o que reverter, então não esperam referência.
	for _, kind := range []Kind{KindBet, KindWin, KindLoss} {
		valor := money.MustParse("10.00", money.BRL)
		if kind == KindLoss {
			valor = money.Zero(money.BRL)
		}
		tx := externa(t, ExternalParams{Kind: kind, Amount: valor})
		if err := tx.AwaitReference(agora, agora); !errors.Is(err, ErrInvalidStateTransition) {
			t.Errorf("%s: esperava ErrInvalidStateTransition, veio %v", kind, err)
		}
	}
}

func TestPendenteDeReferencaEterminal(t *testing.T) {
	tx := externa(t, ExternalParams{Kind: KindRollback, ReferenceExtID: "ext-bet"})
	if err := tx.MarkFailed("X", "x", agora); err != nil {
		t.Fatal(err)
	}
	if err := tx.AwaitReference(agora, agora); !errors.Is(err, ErrTerminalTransition) {
		t.Errorf("esperava ErrTerminalTransition, veio %v", err)
	}
}

func TestBackoffDasTentativasDeReferencia(t *testing.T) {
	tx := externa(t, ExternalParams{Kind: KindRefund, ReferenceExtID: "ext-bet"})
	base := 10 * time.Second

	// Espera cresce exponencialmente até o limite de tentativas.
	for tentativa, quer := range []time.Duration{
		10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second,
	} {
		exhausted, next, err := tx.RegisterReferenceAttempt(base, 6, agora)
		if err != nil {
			t.Fatal(err)
		}
		if exhausted {
			t.Fatalf("tentativa %d esgotou cedo demais", tentativa+1)
		}
		if got := next.Sub(agora); got != quer {
			t.Errorf("tentativa %d: espera = %v, quer %v", tentativa+1, got, quer)
		}
	}
	// Na sexta tentativa a espera acaba e a operação precisa ser rejeitada.
	exhausted, _, err := tx.RegisterReferenceAttempt(base, 6, agora)
	if err != nil {
		t.Fatal(err)
	}
	if !exhausted {
		t.Error("esperava esgotar na sexta tentativa")
	}
	if tx.ReferenceAttempts() != 6 {
		t.Errorf("tentativas = %d, quer 6", tx.ReferenceAttempts())
	}
}

func TestRegisterReferenceAttemptValidaEntrada(t *testing.T) {
	tx := externa(t, ExternalParams{Kind: KindRefund, ReferenceExtID: "ext-bet"})
	if _, _, err := tx.RegisterReferenceAttempt(time.Second, 0, agora); err == nil {
		t.Error("maxAttempts zero deveria falhar")
	}
	bet := externa(t, ExternalParams{Kind: KindBet})
	if _, _, err := bet.RegisterReferenceAttempt(time.Second, 3, agora); !errors.Is(err, ErrInvalidStateTransition) {
		t.Errorf("BET não conta tentativa de referência: %v", err)
	}
}

func TestValidacaoDaReferencia(t *testing.T) {
	refProcessada := func() *Transaction {
		ref := externa(t, ExternalParams{
			ID: "tx-bet", Kind: KindBet, ExternalID: "ext-bet",
			Amount: money.MustParse("10.00", money.BRL),
		})
		if err := ref.MarkProcessed(agora); err != nil {
			t.Fatal(err)
		}
		return ref
	}

	// Alinhado: passa.
	ok := externa(t, ExternalParams{Kind: KindRefund, ReferenceExtID: "ext-bet"})
	if err := ok.ValidateReference(refProcessada()); err != nil {
		t.Errorf("referência alinhada deveria passar: %v", err)
	}

	// Referência não processada.
	pendente := externa(t, ExternalParams{
		ID: "tx-bet", Kind: KindBet, ExternalID: "ext-bet",
		Amount: money.MustParse("10.00", money.BRL),
	})
	casos := []struct {
		nome string
		ref  *Transaction
		want error
	}{
		{"referência não processada", pendente, ErrReferenceNotProcessed},
		{"referência ausente", nil, ErrMissingReference},
		{"provedor diferente", externa(t, ExternalParams{
			ProviderID: "outro", Kind: KindBet, ExternalID: "ext-bet",
			Amount: money.MustParse("10.00", money.BRL), Now: agora,
		}), ErrProviderMismatch},
		{"jogador diferente", externa(t, ExternalParams{
			PlayerID: "p2", Kind: KindBet, ExternalID: "ext-bet",
			Amount: money.MustParse("10.00", money.BRL), Now: agora,
		}), ErrPlayerMismatch},
		{"moeda diferente", externa(t, ExternalParams{
			Kind: KindBet, ExternalID: "ext-bet",
			Amount: money.MustParse("10.00", money.USD), Now: agora,
		}), ErrCurrencyMismatchOnReference},
	}
	for _, c := range casos {
		if c.nome == "referência não processada" {
			if err := ok.ValidateReference(c.ref); !errors.Is(err, c.want) {
				t.Errorf("%s: esperava %v, veio %v", c.nome, c.want, err)
			}
			continue
		}
		// Processa a referência antes de conferir as divergências.
		if c.ref != nil {
			if err := c.ref.MarkProcessed(agora); err != nil {
				t.Fatal(err)
			}
			if err := ok.ValidateReference(c.ref); !errors.Is(err, c.want) {
				t.Errorf("%s: esperava %v, veio %v", c.nome, c.want, err)
			}
		}
	}
}

func TestValidacaoDaReferenciaAceitaAusente(t *testing.T) {
	// nil é a referência ausente.
	tx := externa(t, ExternalParams{Kind: KindRefund, ReferenceExtID: "ext-bet"})
	if err := tx.ValidateReference(nil); !errors.Is(err, ErrMissingReference) {
		t.Errorf("esperava ErrMissingReference, veio %v", err)
	}
}

func TestValorDaReversaoPrecisaBater(t *testing.T) {
	// Reversão é integral: valor diferente é recusado.
	ref := externa(t, ExternalParams{
		ID: "tx-bet", Kind: KindBet, ExternalID: "ext-bet",
		Amount: money.MustParse("10.00", money.BRL),
	})
	if err := ref.MarkProcessed(agora); err != nil {
		t.Fatal(err)
	}
	tx := externa(t, ExternalParams{
		Kind: KindRefund, ReferenceExtID: "ext-bet",
		Amount: money.MustParse("9.00", money.BRL),
	})
	if err := tx.ValidateReference(ref); !errors.Is(err, ErrAmountMismatchOnReference) {
		t.Errorf("esperava ErrAmountMismatchOnReference, veio %v", err)
	}
}

func TestSentidoDoMovimento(t *testing.T) {
	cases := []struct {
		kind    Kind
		dir     ledger.Direction
		movemen bool
	}{
		{KindBet, ledger.Debit, true},
		{KindWin, ledger.Credit, true},
		{KindRefund, ledger.Credit, true},
		{KindOpening, ledger.Credit, true},
		{KindLoss, "", false},
	}
	for _, c := range cases {
		// OPENING é a única origem interna, então não sai de NewExternal.
		var tx *Transaction
		if c.kind == KindOpening {
			tx = abertura(t)
		} else {
			valor := money.MustParse("10.00", money.BRL)
			if c.kind == KindLoss {
				valor = money.Zero(money.BRL)
			}
			// BET e LOSS não aceitam referência; as reversões sim.
			ref := ""
			if c.kind.RequiresReference() {
				ref = "ext-bet"
			}
			tx = externa(t, ExternalParams{Kind: c.kind, Amount: valor, ReferenceExtID: ref})
		}
		dir, movimenta, err := tx.Direction()
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if movimenta != c.movemen {
			t.Errorf("%s: movimenta = %v, quer %v", c.kind, movimenta, c.movemen)
		}
		if c.movemen && dir != c.dir {
			t.Errorf("%s: direção = %s, quer %s", c.kind, dir, c.dir)
		}
	}
	if KindLoss.MovesMoney() {
		t.Error("LOSS não deveria movimentar dinheiro")
	}
}

func TestSentidoDaReversao(t *testing.T) {
	ref := func(kind Kind, valor string) *Transaction {
		var r *Transaction
		if kind == KindOpening {
			// Abertura não vem de fora; o resto sim.
			r = abertura(t)
		} else {
			r = externa(t, ExternalParams{
				ID: "tx-ref", Kind: kind, ExternalID: "ext-ref",
				Amount: money.MustParse(valor, money.BRL), Now: agora,
			})
		}
		if err := r.MarkProcessed(agora); err != nil {
			t.Fatal(err)
		}
		return r
	}
	rollback := func() *Transaction {
		return externa(t, ExternalParams{Kind: KindRollback, ReferenceExtID: "ext-ref"})
	}

	// REFUND é sempre crédito, e a referência não muda isso.
	refund := externa(t, ExternalParams{Kind: KindRefund})
	if dir, err := refund.ReversalDirection(nil); err != nil || dir != ledger.Credit {
		t.Errorf("REFUND: dir=%s err=%v", dir, err)
	}

	// ROLLBACK faz o contrário da operação original.
	casos := []struct {
		ref  *Transaction
		quer ledger.Direction
	}{
		{ref(KindBet, "10.00"), ledger.Credit},
		{ref(KindWin, "10.00"), ledger.Debit},
		{ref(KindRefund, "10.00"), ledger.Debit},
	}
	for i, c := range casos {
		dir, err := rollback().ReversalDirection(c.ref)
		if err != nil {
			t.Fatalf("caso %d: %v", i, err)
		}
		if dir != c.quer {
			t.Errorf("caso %d: direção = %s, quer %s", i, dir, c.quer)
		}
	}

	// LOSS e OPENING não têm movimento a desfazer.
	for _, kind := range []Kind{KindLoss, KindOpening} {
		valor := money.MustParse("10.00", money.BRL)
		if kind == KindLoss {
			valor = money.Zero(money.BRL)
		}
		if _, err := rollback().ReversalDirection(ref(kind, valor.String())); !errors.Is(err, ErrInvalidStateTransition) {
			t.Errorf("%s não deveria ser reversível: %v", kind, err)
		}
	}
	// ROLLBACK sem referência não tem contra o que se mover.
	if _, err := rollback().ReversalDirection(nil); !errors.Is(err, ErrMissingReference) {
		t.Errorf("ROLLBACK sem referência: esperava ErrMissingReference, veio %v", err)
	}
	// BET não é reversão.
	bet := externa(t, ExternalParams{Kind: KindBet})
	if _, err := bet.ReversalDirection(ref(KindBet, "10.00")); !errors.Is(err, ErrInvalidStateTransition) {
		t.Errorf("BET não é reversão: %v", err)
	}
}

func TestIdempotencia(t *testing.T) {
	tx := externa(t, ExternalParams{Kind: KindBet, PayloadHash: "hash-1"})

	// Mesma chave, mesmo payload: reentrega.
	if err := tx.CheckIdempotencyReplay("hash-1"); err != nil {
		t.Errorf("mesma chave e mesmo hash deveria passar: %v", err)
	}
	// Sem registro anterior: ainda passa.
	if err := tx.CheckIdempotencyReplay(""); err != nil {
		t.Errorf("sem hash registrado deveria passar: %v", err)
	}
	// Mesma chave, outro payload: conflito, não reexecução.
	if err := tx.CheckIdempotencyReplay("hash-outro"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Errorf("esperava ErrIdempotencyConflict, veio %v", err)
	}
}

func TestRehydrateNaoRevalida(t *testing.T) {
	tx := Rehydrate(
		"tx-9", KindRefund, StateProcessed,
		"p9", "w9", money.MustParse("10.00", money.BRL),
		"round-9", "game-9", "prov-9", "ext-9", "prov-9:ext-9", "hash-9",
		"ext-0", "tx-0", "", "",
		3, agora,
		money.Zero(money.BRL), false,
		agora, agora, agora, true,
	)
	if tx.ID() != "tx-9" || tx.Kind() != KindRefund || tx.State() != StateProcessed {
		t.Error("identidade não preservada")
	}
	if tx.RoundID() != "round-9" || tx.GameID() != "game-9" {
		t.Error("rodada e jogo não preservados")
	}
	if tx.ReferenceExternalID() != "ext-0" || tx.ReferenceID() != "tx-0" {
		t.Error("referências não preservadas")
	}
	if tx.ReferenceAttempts() != 3 {
		t.Errorf("tentativas = %d", tx.ReferenceAttempts())
	}
	if !tx.HasProcessedAt() {
		t.Error("processedAt perdido")
	}
}

func TestStateEKindValidos(t *testing.T) {
	for _, s := range []State{StatePending, StatePendingReference, StateProcessed, StateRejected, StateFailed} {
		if !s.Valid() {
			t.Errorf("estado %s deveria ser válido", s)
		}
	}
	for _, s := range []State{"", "PENDING ", "DONE", "processed"} {
		if s.Valid() {
			t.Errorf("estado %q deveria ser inválido", s)
		}
	}
	for _, k := range []Kind{KindOpening, KindBet, KindWin, KindLoss, KindRefund, KindRollback} {
		if !k.Valid() {
			t.Errorf("tipo %s deveria ser válido", k)
		}
	}
	for _, k := range []Kind{"", "BET ", "cashback"} {
		if k.Valid() {
			t.Errorf("tipo %q deveria ser inválido", k)
		}
	}
}
