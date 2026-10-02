// Package fxapp monta a camada de serviço, expondo os casos de uso por
// interface para os handlers.
package fxapp

import (
	"go.uber.org/fx"

	"github.com/shimigui/go-challenge/internal/domain/wager"
	"github.com/shimigui/go-challenge/internal/domain/wallet"
	"github.com/shimigui/go-challenge/internal/service"
)

var Module = fx.Module("service",
	fx.Provide(
		fx.Annotate(
			service.NewWalletService,
			fx.As(new(wallet.Service)),
		),
		fx.Annotate(
			service.NewWageringService,
			fx.As(new(wager.Service)),
		),
	),
)
