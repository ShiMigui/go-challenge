package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shimigui/go-challenge/internal/config"
)

// OpenDB abre o pool de conexões com o driver pgx.
//
// O driver é registrado no wiring (cmd/api faz o import em branco de
// pgx/stdlib); aqui só configuramos o pool e conferimos a conexão com um
// ping antes de entregar o *sql.DB para o grafo Fx.
func OpenDB(cfg config.DB) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("abrir banco: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping no banco: %w", err)
	}
	return db, nil
}
