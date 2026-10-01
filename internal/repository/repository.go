// Package repository traz a persistência dos agregados.
//
// Os repositórios falam com o Postgres por database/sql, sem driver
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
)

var (
	// ErrNotFound é devolvido quando o registro não existe.
	ErrNotFound = errors.New("registro nao encontrado")
	// ErrOptimisticLock é devolvido quando a versão mudou desde a leitura.
	ErrOptimisticLock = errors.New("concorrencia otimista: versao desatualizada")
	// ErrInvalidID é devolvido quando um identificador não é UUID válido.
	ErrInvalidID = errors.New("identificador nao e um UUID valido")
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

// validUUID confere o formato 8-4-4-4-12 em hexadecimal.
//
// A checagem existe para devolver um erro claro. Sem ela, uma string
// inválida chega ao Postgres e volta como erro de cast, que parece falha
// de banco em vez de entrada ruim.
func validUUID(field, value string) error {
	if !isUUID(value) {
		return fmt.Errorf("%w: %s = %q", ErrInvalidID, field, value)
	}
	return nil
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
			continue
		}
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
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
