// Package bootstrap monta a composição raiz da aplicação com Fx.
//
// É o único lugar que conhece todos os módulos: application (casos de
// uso), infrastructure (banco e health) e interfaces (HTTP). O cmd/api
// só chama Options e roda o grafo.
package bootstrap

import (
	"context"
	"log"
	"net/http"
	"time"

	"go.uber.org/fx"

	applicationfx "github.com/shimigui/go-challenge/internal/application/fxapp"
	"github.com/shimigui/go-challenge/internal/config"
	"github.com/shimigui/go-challenge/internal/infrastructure/health"
	"github.com/shimigui/go-challenge/internal/infrastructure/persistence"
	httpfx "github.com/shimigui/go-challenge/internal/interfaces/http/fxapp"
)

// Options devolve as opções Fx da aplicação inteira, na ordem de camadas:
// configuração, portas de banco e transação, verificação de prontidão,
// casos de uso e interface HTTP.
func Options() []fx.Option {
	return []fx.Option{
		fx.Provide(config.Load),
		fx.Provide(func(cfg config.Config) config.DB { return cfg.DB }),
		fx.Provide(persistence.OpenDB),

		persistence.Module,
		health.Module,
		applicationfx.Module,
		httpfx.Module,

		// O servidor HTTP entra no ciclo de vida: sobe junto com o app,
		// desce com shutdown gracioso na parada.
		fx.Invoke(lifecycle),
	}
}

// lifecycle liga o servidor HTTP ao ciclo de vida do Fx.
func lifecycle(lc fx.Lifecycle, cfg config.Config, handler http.Handler) {
	srv := &http.Server{
		Addr:              ":" + cfg.API.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Printf("http: servidor encerrou com erro: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
}
