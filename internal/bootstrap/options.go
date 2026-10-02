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
	"github.com/shimigui/go-challenge/internal/application/wagering"
	"github.com/shimigui/go-challenge/internal/config"
	"github.com/shimigui/go-challenge/internal/infrastructure/health"
	"github.com/shimigui/go-challenge/internal/infrastructure/persistence"
	httpfx "github.com/shimigui/go-challenge/internal/interfaces/http/fxapp"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

// Options devolve as opções Fx da aplicação inteira, na ordem de camadas:
// configuração, portas de banco e transação, verificação de prontidão,
// casos de uso e interface HTTP.
func Options() []fx.Option {
	return []fx.Option{
		fx.Provide(func() config.Config { return config.MustLoad() }),
		fx.Provide(func(cfg config.Config) config.DB { return cfg.DB }),

		persistence.Module,
		health.Module,
		applicationfx.Module,
		httpfx.Module,

		// O servidor HTTP entra no ciclo de vida: sobe junto com o app,
		// desce com shutdown gracioso na parada.
		fx.Invoke(lifecycle),
	}
}

// lifecycle liga o servidor HTTP e o worker de referências ao ciclo de
// vida do Fx: sobem junto com o app, descem com shutdown gracioso na parada.
func lifecycle(lc fx.Lifecycle, cfg config.Config, handler http.Handler, worker *wagering.ReferenceWorker) {
	// Configura o ambiente para controle de mensagens de erro
	middleware.SetAppEnv(cfg.AppEnv)

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

			// O worker roda até o shutdown: o contexto derivado é cancelado
			// no OnStop, encerrando a varredura de pendências.
			wctx, cancel := context.WithCancel(context.Background())
			go worker.Loop(wctx)
			lc.Append(fx.Hook{
				OnStop: func(ctx context.Context) error {
					cancel()
					return nil
				},
			})
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
}
