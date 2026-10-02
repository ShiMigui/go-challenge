// Package repository traz a persistência dos agregados.
//
// Os repositórios falam com o banco por database/sql, sem driver
// importado: quem registra o driver é o wiring, não o domínio. Isso deixa
// o go.mod sob controle e mantém estas funções testáveis com qualquer
// driver compatível.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
)

var (
	// ErrNotFound é devolvido quando o registro não existe.
	ErrNotFound = errors.New("registro nao encontrado")
	// ErrOptimisticLock é devolvido quando a versão mudou desde a leitura.
	ErrOptimisticLock = errors.New("concorrencia otimista: versao desatualizada")
	// ErrMessageTampered é devolvido quando a reentrega traz outro corpo.
	ErrMessageTampered = errors.New("mensagem reentregue com hash diferente")
	// ErrAlreadyCompleted é devolvido ao completar mensagem já concluída.
	ErrAlreadyCompleted = errors.New("mensagem ja concluida")
)

// Querier é o que *sql.DB e *sql.Tx têm em comum.
//
// Aceitar essa interface permite que o mesmo repositório rode fora de
// transação (leitura) ou dentro dela (escrita), sem duas implementações.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// validUUID confere o formato do identificador antes de tocar o banco.
//
// A checagem existe para devolver um erro claro. Sem ela, uma string
// inválida chega ao driver e volta como erro de cast, que parece falha de
// infraestrutura em vez de entrada ruim.
func validUUID(field, value string) error {
	if !identifier.IsValid(value) {
		return fmt.Errorf("%w: %s = %q", identifier.ErrInvalidID, field, value)
	}
	return nil
}

// nullString transforma string vazia em NULL.
//
// O banco trata coluna vazia e coluna nula como a mesma coisa em vários
// lugares, e as constraints esperam NULL, não ”.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// stringFromNull devolve "" para NULL.
func stringFromNull(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

// timePtr devolve o ponteiro que o Scan espera para TIMESTAMPTZ anulável.
func timePtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	t := nt.Time
	return &t
}
