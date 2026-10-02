// Package fxapp monta a camada HTTP: handlers e rotas.
//
// Os serviços entram por interface (wallet.Service, wager.Service),
// fornecidos pelo módulo de service. A ordem de fornecimento não importa:
// o Fx resolve o grafo no início.
package fxapp

import (
	"go.uber.org/fx"

	"github.com/shimigui/go-challenge/internal/api/handler"
	"github.com/shimigui/go-challenge/internal/api/server"
)

var Module = fx.Module("api",
	fx.Provide(
		handler.NewWalletHandler,
		handler.NewWageringHandler,
		handler.NewHealthHandler,
		server.NewRouter,
	),
)
