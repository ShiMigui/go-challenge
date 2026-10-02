// Package fxapp monta a camada HTTP: handlers e rotas.
//
// Os serviços entram por interface (wallet.Service, wagering.Service),
// fornecidos pelo módulo de application. A ordem de fornecimento não
// importa: o Fx resolve o grafo no início.
package fxapp

import (
	"go.uber.org/fx"

	"github.com/shimigui/go-challenge/internal/interfaces/http/handler"
	"github.com/shimigui/go-challenge/internal/interfaces/http/server"
)

var Module = fx.Module("api",
	fx.Provide(
		handler.NewWalletHandler,
		handler.NewWageringHandler,
		handler.NewHealthHandler,
		server.NewRouter,
	),
)
