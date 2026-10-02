// Package fxapp monta a camada de aplicação, expondo os casos de uso por
// interface para os handlers.
package fxapp

import (
	"go.uber.org/fx"

	"github.com/shimigui/go-challenge/internal/application/wagering"
	"github.com/shimigui/go-challenge/internal/application/wallet"
)

var Module = fx.Module("application",
	fx.Provide(
		fx.Annotate(
			wallet.NewService,
			fx.As(new(wallet.Service)),
		),
		fx.Annotate(
			wagering.NewService,
			fx.As(new(wagering.Service)),
		),
		// O worker de referências retoma as operações PENDING_REFERENCE.
		// Fica no grafo para o bootstrap ligá-lo ao ciclo de vida.
		wagering.NewReferenceWorker,
	),
)
