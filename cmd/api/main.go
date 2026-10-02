// Command api é o entrypoint da aplicação: monta o grafo Fx a partir do
// bootstrap e o roda até o encerramento.
package main

import (
	"go.uber.org/fx"

	"github.com/shimigui/go-challenge/internal/bootstrap"

	// Registra o driver do banco (pgx sobre database/sql). O import é em
	// branco de propósito: o wiring é o único dono do driver.
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	fx.New(bootstrap.Options()...).Run()
}
