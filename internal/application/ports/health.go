package ports

import "context"

// HealthChecker verifica a prontidão das dependências da aplicação.
//
// O handler de health depende deste port; a infraestrutura implementa.
type HealthChecker interface {
	// CheckDatabase pinga o banco de dados.
	CheckDatabase(ctx context.Context) error
	// CheckMessaging verifica a fila de mensagens.
	CheckMessaging(ctx context.Context) error
}
