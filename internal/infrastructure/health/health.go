// Package health implementa a verificação de prontidão das dependências.
package health

import (
	"context"
	"database/sql"
	"errors"

	"github.com/shimigui/go-challenge/internal/application/ports"
)

// checker implementa ports.HealthChecker.
type checker struct {
	db *sql.DB
}

// New devolve o port de health ligado ao banco.
func New(db *sql.DB) ports.HealthChecker {
	return &checker{db: db}
}

// CheckDatabase pinga o banco de dados.
func (c *checker) CheckDatabase(ctx context.Context) error {
	return c.db.PingContext(ctx)
}

// CheckMessaging verifica a fila de mensagens.
//
// A fila ainda não entrou no wiring: sem consumidor ativo, a dependência
// é tratada como ausente (down) em vez de silenciosamente saudável.
func (c *checker) CheckMessaging(ctx context.Context) error {
	return errors.New("mensageria nao configurada")
}
