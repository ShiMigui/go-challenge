// Package wager modela a transação de wagering.
//
// É o agregado que coordena operação, estado e referência. A máquina de
// estados é validada aqui e reforçada por CHECK no banco.
package wager

import (
	"errors"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
)

// Kind é o tipo da operação de wagering.
type Kind string

const (
	// KindOpening é a abertura interna de carteira. Rejeitado quando vem de fora.
	KindOpening Kind = "OPENING"
	// KindBet é a aposta: débito, exige saldo.
	KindBet Kind = "BET"
	// KindWin é o prêmio: crédito.
	KindWin Kind = "WIN"
	// KindLoss é a derrota: não movimenta dinheiro.
	KindLoss Kind = "LOSS"
	// KindRefund devolve integralmente uma aposta.
	KindRefund Kind = "REFUND"
	// KindRollback desfaz uma operação processada, no sentido contrário.
	KindRollback Kind = "ROLLBACK"
)

// State é o estado do ciclo de vida da transação.
type State string

const (
	// StatePending: registro aceito, processamento não concluído.
	StatePending State = "PENDING"
	// StatePendingReference: depende de referência ainda indisponível.
	StatePendingReference State = "PENDING_REFERENCE"
	// StateProcessed: concluída com sucesso. Terminal.
	StateProcessed State = "PROCESSED"
	// StateRejected: recusada por regra de negócio. Terminal.
	StateRejected State = "REJECTED"
	// StateFailed: falha permanente de infraestrutura. Terminal.
	StateFailed State = "FAILED"
)

var (
	// ErrTerminalTransition é devolvido ao tentar alterar estado terminal.
	ErrTerminalTransition = errors.New("transacao terminal nao aceita transicao")
	// ErrInvalidKind é devolvido para tipo desconhecido.
	ErrInvalidKind = errors.New("tipo de operacao invalido")
	// ErrInvalidStateTransition é devolvido para transição não permitida.
	ErrInvalidStateTransition = errors.New("transicao de estado invalida")
	// ErrMissingReference é devolvido em reversão sem referência.
	ErrMissingReference = errors.New("REFUND e ROLLBACK exigem referencia externa")
	// ErrForbiddenReference é devolvido quando BET ou LOSS trazem referência.
	ErrForbiddenReference = errors.New("BET e LOSS nao aceitam referencia")
	// ErrInvalidAmountForKind é devolvido quando o valor não bate com o tipo.
	ErrInvalidAmountForKind = errors.New("valor incompativel com o tipo da operacao")
	// ErrFailureCodeRequired é devolvido ao finalizar sem código de falha.
	ErrFailureCodeRequired = errors.New("finalizacao exige codigo de falha")
	// ErrPlayerMismatch é devolvido quando jogador diverge da referência.
	ErrPlayerMismatch = errors.New("jogador difere do da referencia")
	// ErrProviderMismatch é devolvido quando provedor diverge da referência.
	ErrProviderMismatch = errors.New("provedor difere do da referencia")
	// ErrAmountMismatchOnReference é devolvido quando o valor diverge.
	ErrAmountMismatchOnReference = errors.New("valor difere do da referencia")
	// ErrCurrencyMismatchOnReference é devolvido quando moeda diverge.
	ErrCurrencyMismatchOnReference = errors.New("moeda difere do da referencia")
	// ErrReferenceNotProcessed é devolvido quando a referência não pode ser desfeita.
	ErrReferenceNotProcessed = errors.New("referencia nao esta processada")
	// ErrIdempotencyConflict é devolvido quando a mesma chave traz outro payload.
	ErrIdempotencyConflict = errors.New("chave de idempotencia reaproveitada com outro payload")
	// ErrInvalidPlayerID é devolvido sem jogador.
	ErrInvalidPlayerID = errors.New("player_id obrigatorio")
	// ErrInvalidWalletID é devolvido sem carteira.
	ErrInvalidWalletID = errors.New("wallet_id obrigatorio")
	// ErrInvalidTransactionID é devolvido sem identidade de transação.
	ErrInvalidTransactionID = errors.New("transaction_id obrigatorio")
	// ErrInvalidProviderID é devolvido sem provedor.
	ErrInvalidProviderID = errors.New("provider_id obrigatorio")
	// ErrInvalidExternalID é devolvido sem id externo.
	ErrInvalidExternalID = errors.New("external_transaction_id obrigatorio")
	// ErrInvalidIdempotencyKey é devolvido sem chave de idempotência.
	ErrInvalidIdempotencyKey = errors.New("idempotency_key obrigatorio")
	// ErrInvalidPayloadHash é devolvido sem hash de payload.
	ErrInvalidPayloadHash = errors.New("payload_hash obrigatorio")
	// ErrExternalNotAllowed é devolvido quando uma operação externa tenta ser
	// criada sem ser uma transação de entrada.
	ErrExternalNotAllowed = errors.New("operacao externa nao pode ser OPENING")
)

// IsTerminal informa se o estado é final.
func (s State) IsTerminal() bool {
	return s == StateProcessed || s == StateRejected || s == StateFailed
}

// Valid informa se o estado é conhecido.
func (s State) Valid() bool {
	switch s {
	case StatePending, StatePendingReference, StateProcessed, StateRejected, StateFailed:
		return true
	}
	return false
}

// String devolve o valor do enum.
func (s State) String() string { return string(s) }

// IsExternal informa se o tipo veio de um provedor.
func (k Kind) IsExternal() bool { return k != KindOpening }

// MovesMoney informa se o tipo altera o saldo.
//
// LOSS não altera: o dinheiro já saiu na aposta. Registrar um débito aqui
// cobraria a mesma perda duas vezes.
func (k Kind) MovesMoney() bool { return k != KindLoss }

// RequiresReference informa se o tipo não pode existir sem referência.
func (k Kind) RequiresReference() bool {
	return k == KindRefund || k == KindRollback
}

// AllowsReference informa se o tipo pode citar uma referência.
//
// WIN pode citar a aposta da rodada; BET e LOSS não, porque não têm o que
// reverter nem a que valor se ligar.
func (k Kind) AllowsReference() bool {
	return k == KindWin || k == KindRefund || k == KindRollback
}

// Valid informa se o tipo é conhecido.
func (k Kind) Valid() bool {
	switch k {
	case KindOpening, KindBet, KindWin, KindLoss, KindRefund, KindRollback:
		return true
	}
	return false
}

// String devolve o valor do enum.
func (k Kind) String() string { return string(k) }

// Transaction é o agregado de uma operação de wagering.
type Transaction struct {
	id          string
	kind        Kind
	state       State
	playerID    string
	walletID    string
	amount      money.Money // contém a moeda
	roundID     string
	gameID      string
	providerID  string
	externalID  string
	idempotency string
	payloadHash string

	referenceExternalID string
	referenceID         string

	failureCode    string
	failureMessage string

	referenceAttempts    int
	referenceNextAttempt time.Time

	createdAt    time.Time
	updatedAt    time.Time
	processedAt  time.Time
	hasProcessed bool
}

// OpeningParams são os dados de uma abertura interna de carteira.
// A moeda vem do Amount.Currency().
type OpeningParams struct {
	ID       string
	PlayerID string
	WalletID string
	Amount   money.Money
	Now      time.Time
}

// NewOpening cria a abertura interna de uma carteira.
//
// É a única origem interna: não tem provedor, id externo, chave, hash,
// rodada, jogo nem referência, e não pode ser criada a partir de HTTP ou SQS.
func NewOpening(p OpeningParams) (*Transaction, error) {
	if p.ID == "" {
		return nil, ErrInvalidTransactionID
	}
	if p.PlayerID == "" {
		return nil, ErrInvalidPlayerID
	}
	if p.WalletID == "" {
		return nil, ErrInvalidWalletID
	}
	if p.Amount.IsNegative() {
		return nil, fmt.Errorf("%w: abertura %s", money.ErrNegativeAmount, p.Amount)
	}
	// Abertura não movimenta por BET/WIN: é um crédito inicial e pode ser zero.
	return &Transaction{
		id:        p.ID,
		kind:      KindOpening,
		state:     StatePending,
		playerID:  p.PlayerID,
		walletID:  p.WalletID,
		amount:    p.Amount,
		createdAt: p.Now,
		updatedAt: p.Now,
	}, nil
}

// ExternalParams são os dados de uma operação vinda de um provedor.
type ExternalParams struct {
	ID             string
	ProviderID     string
	ExternalID     string
	IdempotencyKey string
	PayloadHash    string
	PlayerID       string
	WalletID       string
	Kind           Kind
	Amount         money.Money // contém a moeda
	RoundID        string
	GameID         string
	ReferenceExtID string
	Now            time.Time
}

// NewExternal cria uma operação de um provedor.
func NewExternal(p ExternalParams) (*Transaction, error) {
	if p.ID == "" {
		return nil, ErrInvalidTransactionID
	}
	if !p.Kind.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidKind, string(p.Kind))
	}
	// OPENING é reservada à abertura interna. Chegar aqui significa que veio
	// de HTTP ou SQS, e precisa ser recusado.
	if p.Kind == KindOpening {
		return nil, ErrExternalNotAllowed
	}
	if p.ProviderID == "" {
		return nil, ErrInvalidProviderID
	}
	if p.ExternalID == "" {
		return nil, ErrInvalidExternalID
	}
	if p.IdempotencyKey == "" {
		return nil, ErrInvalidIdempotencyKey
	}
	if p.PayloadHash == "" {
		return nil, ErrInvalidPayloadHash
	}
	if p.PlayerID == "" {
		return nil, ErrInvalidPlayerID
	}
	if p.WalletID == "" {
		return nil, ErrInvalidWalletID
	}
	// A moeda da transação vem do Amount
	if err := validateAmountForKind(p.Kind, p.Amount); err != nil {
		return nil, err
	}
	if err := validateReference(p.Kind, p.ReferenceExtID); err != nil {
		return nil, err
	}
	return &Transaction{
		id:                  p.ID,
		kind:                p.Kind,
		state:               StatePending,
		playerID:            p.PlayerID,
		walletID:            p.WalletID,
		amount:              p.Amount,
		roundID:             p.RoundID,
		gameID:              p.GameID,
		providerID:          p.ProviderID,
		externalID:          p.ExternalID,
		idempotency:         p.IdempotencyKey,
		payloadHash:         p.PayloadHash,
		referenceExternalID: p.ReferenceExtID,
		createdAt:           p.Now,
		updatedAt:           p.Now,
	}, nil
}

// validateAmountForKind aplica a regra de valor por tipo.
//
// LOSS exige zero. Todo movimento exige valor positivo. Zero é aceito só na
// abertura inicial e em LOSS, como a spec determina.
func validateAmountForKind(kind Kind, amount money.Money) error {
	if kind == KindLoss {
		if !amount.IsZero() {
			return fmt.Errorf("%w: LOSS exige 0.00, veio %s", ErrInvalidAmountForKind, amount)
		}
		return nil
	}
	if !amount.IsPositive() {
		return fmt.Errorf("%w: %s exige valor positivo, veio %s", ErrInvalidAmountForKind, kind, amount)
	}
	return nil
}

// validateReference exige referência nas reversões e a proíbe onde não faz sentido.
func validateReference(kind Kind, ref string) error {
	if kind.RequiresReference() && ref == "" {
		return fmt.Errorf("%w: %s", ErrMissingReference, kind)
	}
	if !kind.AllowsReference() && ref != "" {
		return fmt.Errorf("%w: %s", ErrForbiddenReference, kind)
	}
	return nil
}

// ID devolve a identidade interna.
func (t *Transaction) ID() string { return t.id }

// Kind devolve o tipo da operação.
func (t *Transaction) Kind() Kind { return t.kind }

// State devolve o estado atual.
func (t *Transaction) State() State { return t.state }

// PlayerID devolve o jogador.
func (t *Transaction) PlayerID() string { return t.playerID }

// WalletID devolve a carteira afetada.
func (t *Transaction) WalletID() string { return t.walletID }

// Currency devolve a moeda da operação (do amount).
func (t *Transaction) Currency() money.Currency { return t.amount.Currency() }

// Amount devolve o valor da operação.
func (t *Transaction) Amount() money.Money { return t.amount }

// RoundID devolve a rodada, vazia em OPENING.
func (t *Transaction) RoundID() string { return t.roundID }

// GameID devolve o jogo, vazio em OPENING.
func (t *Transaction) GameID() string { return t.gameID }

// ProviderID devolve o provedor, vazio em OPENING.
func (t *Transaction) ProviderID() string { return t.providerID }

// ExternalID devolve o id externo, vazio em OPENING.
func (t *Transaction) ExternalID() string { return t.externalID }

// IdempotencyKey devolve a chave de deduplicação.
func (t *Transaction) IdempotencyKey() string { return t.idempotency }

// PayloadHash devolve o hash do payload.
func (t *Transaction) PayloadHash() string { return t.payloadHash }

// ReferenceExternalID devolve o id externo da operação referenciada.
func (t *Transaction) ReferenceExternalID() string { return t.referenceExternalID }

// ReferenceID devolve a referência interna resolvida.
func (t *Transaction) ReferenceID() string { return t.referenceID }

// FailureCode devolve o código de falha.
func (t *Transaction) FailureCode() string { return t.failureCode }

// FailureMessage devolve o detalhe da falha.
func (t *Transaction) FailureMessage() string { return t.failureMessage }

// ReferenceAttempts devolve as tentativas do worker de referências.
func (t *Transaction) ReferenceAttempts() int { return t.referenceAttempts }

// ReferenceNextAttempt devolve o instante da próxima tentativa.
func (t *Transaction) ReferenceNextAttempt() time.Time { return t.referenceNextAttempt }

// CreatedAt devolve o instante de criação.
func (t *Transaction) CreatedAt() time.Time { return t.createdAt }

// UpdatedAt devolve o instante da última alteração.
func (t *Transaction) UpdatedAt() time.Time { return t.updatedAt }

// ProcessedAt devolve o instante de conclusão, zero se não houver.
func (t *Transaction) ProcessedAt() time.Time { return t.processedAt }

// HasProcessedAt informa se a conclusão já foi carimbada.
func (t *Transaction) HasProcessedAt() bool { return t.hasProcessed }

// IsExternal informa se a operação veio de um provedor.
func (t *Transaction) IsExternal() bool { return t.kind.IsExternal() }

// MovesMoney informa se a operação altera o saldo.
func (t *Transaction) MovesMoney() bool { return t.kind.MovesMoney() }

// IsPending informa se a operação ainda está em aberto.
func (t *Transaction) IsPending() bool {
	return t.state == StatePending || t.state == StatePendingReference
}

// MarkProcessed marca a operação como concluída.
//
// Só é válida a partir de PENDING ou PENDING_REFERENCE: é a transição que
// a referencia pendente precisa fazer quando finalmente chega.
func (t *Transaction) MarkProcessed(now time.Time) error {
	return t.transitionToTerminal(StateProcessed, "", "", now)
}

// MarkRejected recusa a operação por regra de negócio.
func (t *Transaction) MarkRejected(code, message string, now time.Time) error {
	return t.transitionToTerminal(StateRejected, code, message, now)
}

// MarkFailed registra falha permanente de infraestrutura.
//
// A distinção importa para o consumidor: FAILED é auditoria de um problema
// nosso, REJECTED é resposta correta a uma entrada que não devia passar.
func (t *Transaction) MarkFailed(code, message string, now time.Time) error {
	return t.transitionToTerminal(StateFailed, code, message, now)
}

// transitionToTerminal aplica uma transição para estado terminal.
//
// PROCESSED não exige código/mensagem; REJECTED e FAILED exigem.
func (t *Transaction) transitionToTerminal(state State, code, message string, now time.Time) error {
	if t.state.IsTerminal() {
		return fmt.Errorf("%w: %s -> %s", ErrTerminalTransition, t.state, state)
	}
	if state != StateProcessed && code == "" {
		return ErrFailureCodeRequired
	}
	t.state = state
	if state == StateProcessed {
		t.failureCode = ""
		t.failureMessage = ""
	} else {
		t.failureCode = code
		t.failureMessage = message
	}
	t.processedAt = now
	t.hasProcessed = true
	t.updatedAt = now
	return nil
}

// AwaitReference coloca a operação em espera pela referência.
//
// A pendência fica persistida e durável: o worker de referências assume a
// partir daqui, mesmo após reinício.
func (t *Transaction) AwaitReference(nextAttempt time.Time, now time.Time) error {
	if t.state.IsTerminal() {
		return fmt.Errorf("%w: %s -> %s", ErrTerminalTransition, t.state, StatePendingReference)
	}
	if !t.kind.RequiresReference() {
		return fmt.Errorf("%w: %s não espera referência", ErrInvalidStateTransition, t.kind)
	}
	t.state = StatePendingReference
	t.referenceNextAttempt = nextAttempt
	t.updatedAt = now
	return nil
}

// ResolveReference associa a referência interna já resolvida.
func (t *Transaction) ResolveReference(referenceID string, now time.Time) error {
	if t.state.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrTerminalTransition, t.state)
	}
	if referenceID == "" {
		return fmt.Errorf("%w: referência interna vazia", ErrMissingReference)
	}
	t.referenceID = referenceID
	t.updatedAt = now
	return nil
}

// RegisterReferenceAttempt conta uma tentativa do worker e devolve o
// próximo prazo com backoff exponencial.
//
// A espera é limitada por maxAttempts: passado o limite, a operação vai para
// REJECTED em vez de ficar esperando para sempre.
func (t *Transaction) RegisterReferenceAttempt(base time.Duration, maxAttempts int, now time.Time) (exhausted bool, next time.Time, err error) {
	if !t.kind.RequiresReference() {
		return false, time.Time{}, fmt.Errorf("%w: %s não espera referência", ErrInvalidStateTransition, t.kind)
	}
	if maxAttempts <= 0 {
		return false, time.Time{}, fmt.Errorf("maxAttempts deve ser positivo, veio %d", maxAttempts)
	}
	t.referenceAttempts++
	t.updatedAt = now
	if t.referenceAttempts >= maxAttempts {
		return true, time.Time{}, nil
	}
	// Backoff exponencial: base * 2^(tentativa-1).
	wait := base
	for i := 1; i < t.referenceAttempts; i++ {
		wait *= 2
	}
	t.referenceNextAttempt = now.Add(wait)
	return false, t.referenceNextAttempt, nil
}

// ValidateReference confere se a referência pode ser revertida por esta
// operação.
//
// A operação e a referência precisam concordar em provedor, jogador, moeda e
// valor, e a referência precisa estar processada.
func (t *Transaction) ValidateReference(ref *Transaction) error {
	if !t.kind.RequiresReference() {
		return fmt.Errorf("%w: %s não reverte", ErrInvalidStateTransition, t.kind)
	}
	if ref == nil {
		return fmt.Errorf("%w: referência ausente", ErrMissingReference)
	}
	if ref.state != StateProcessed {
		return fmt.Errorf("%w: %s está %s", ErrReferenceNotProcessed, ref.externalID, ref.state)
	}
	if ref.providerID != t.providerID {
		return fmt.Errorf("%w: referência de outro provedor (%s vs %s)",
			ErrProviderMismatch, ref.providerID, t.providerID)
	}
	if ref.playerID != t.playerID {
		return fmt.Errorf("%w: %s vs %s", ErrPlayerMismatch, ref.playerID, t.playerID)
	}
	if ref.Currency() != t.Currency() {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatchOnReference, ref.Currency(), t.Currency())
	}
	// Reversão é integral: o valor precisa ser o referenciado.
	equal, err := t.amount.Cmp(ref.amount)
	if err != nil {
		return err
	}
	if equal != 0 {
		return fmt.Errorf("%w: %s vs %s", ErrAmountMismatchOnReference, t.amount, ref.amount)
	}
	return nil
}

// Direction devolve o sentido do movimento que a operação provoca.
//
// LOSS não movimenta dinheiro, então devolve false.
func (t *Transaction) Direction() (ledger.Direction, bool, error) {
	switch t.kind {
	case KindBet:
		return ledger.Debit, true, nil
	case KindWin, KindRefund:
		return ledger.Credit, true, nil
	case KindOpening:
		return ledger.Credit, true, nil
	case KindLoss:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("%w: %s não define sentido", ErrInvalidKind, t.kind)
	}
}

// ReversalDirection devolve o sentido do movimento que a reversão provoca.
//
// REFUND é sempre crédito. ROLLBACK faz o contrário da operação original, o
// que exige olhar a referência: desfazer um BET credita, desfazer um WIN ou
// um REFUND debita.
func (t *Transaction) ReversalDirection(ref *Transaction) (ledger.Direction, error) {
	if t.kind == KindRefund {
		return ledger.Credit, nil
	}
	if t.kind != KindRollback {
		return "", fmt.Errorf("%w: %s não é reversão", ErrInvalidStateTransition, t.kind)
	}
	if ref == nil {
		return "", fmt.Errorf("%w: ROLLBACK exige a referência", ErrMissingReference)
	}
	switch ref.kind {
	case KindBet:
		return ledger.Credit, nil
	case KindWin, KindRefund:
		return ledger.Debit, nil
	case KindOpening, KindLoss:
		return "", fmt.Errorf("%w: %s não gera movimento a desfazer", ErrInvalidStateTransition, ref.kind)
	default:
		return "", fmt.Errorf("%w: referência %s tem tipo inesperado", ErrInvalidKind, ref.kind)
	}
}

// CheckIdempotencyReplay confere se uma reentrega com a mesma chave traz o
// mesmo payload.
//
// Divergência é conflito, não reexecução: a operação já foi processada com
// outro conteúdo e responder de novo alteraria o resultado.
// seenPayloadHash vazio = sem registro anterior, passa.
func (t *Transaction) CheckIdempotencyReplay(seenPayloadHash string) error {
	if seenPayloadHash != "" && seenPayloadHash != t.payloadHash {
		return fmt.Errorf("%w: chave %s com hashes %s e %s",
			ErrIdempotencyConflict, t.idempotency, seenPayloadHash, t.payloadHash)
	}
	return nil
}

// Rehydrate recria a transação a partir do banco.
//
// Não revalida nem reexecuta transição: quem gravou já cumpriu as regras.
func Rehydrate(
	id string, kind Kind, state State,
	playerID, walletID string, amount money.Money,
	roundID, gameID, providerID, externalID, idempotency, payloadHash string,
	referenceExtID, referenceID, failureCode, failureMessage string,
	referenceAttempts int, referenceNextAttempt time.Time,
	createdAt, updatedAt, processedAt time.Time, hasProcessedAt bool,
) *Transaction {
	return &Transaction{
		id: id, kind: kind, state: state,
		playerID: playerID, walletID: walletID,
		amount:  amount,
		roundID: roundID, gameID: gameID,
		providerID: providerID, externalID: externalID,
		idempotency: idempotency, payloadHash: payloadHash,
		referenceExternalID: referenceExtID, referenceID: referenceID,
		failureCode: failureCode, failureMessage: failureMessage,
		referenceAttempts:    referenceAttempts,
		referenceNextAttempt: referenceNextAttempt,
		createdAt:            createdAt,
		updatedAt:            updatedAt,
		processedAt:          processedAt,
		hasProcessed:         hasProcessedAt,
	}
}
