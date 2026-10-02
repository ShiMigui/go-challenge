package health

import "go.uber.org/fx"

// Module expõe o port de health para o grafo Fx.
var Module = fx.Module("health",
	fx.Provide(New),
)
