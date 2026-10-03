// Package main implementa o CLI de migrations (golang-migrate).
//
// Uso:
//
//	go run ./cmd/migrate up           # aplica todas as migrations pendentes
//	go run ./cmd/migrate down         # reverte tudo
//	go run ./cmd/migrate down 1       # reverte 1 migration
//	go run ./cmd/migrate status       # mostra versão atual e pendentes
//	go run ./cmd/migrate force 3      # força versão (emergência)
//	go run ./cmd/migrate version      # mostra versão atual
//
// Variáveis de ambiente:
//
//	DATABASE_URL      string de conexão PostgreSQL
//	MIGRATIONS_PATH   diretório com arquivos .up.sql/.down.sql
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shimigui/go-challenge/internal/infrastructure/persistence"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	// Config
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://wagering:wagering@localhost:5432/wagering?sslmode=disable"
	}
	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "./scripts/postgres/migrations"
	}

	ctx := context.Background()

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro conectando ao banco: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Erro ping banco: %v\n", err)
		os.Exit(1)
	}

	runner := persistence.NewMigrationRunner(db, migrationsPath)

	switch cmd {
	case "up":
		err = runner.Up(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao aplicar migrations: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Migrations aplicadas com sucesso")

	case "down":
		steps := 0
		if len(args) > 0 {
			steps, err = strconv.Atoi(args[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Número de steps inválido: %v\n", err)
				os.Exit(1)
			}
		}
		err = runner.Down(ctx, steps)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao reverter migrations: %v\n", err)
			os.Exit(1)
		}
		if steps > 0 {
			fmt.Printf("✓ %d migration(s) revertida(s)\n", steps)
		} else {
			fmt.Println("✓ Todas as migrations revertidas")
		}

	case "status":
		err = runner.Status(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao obter status: %v\n", err)
			os.Exit(1)
		}

	case "version":
		v, dirty, err := runner.Version(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao obter versão: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Versão atual: %d", v)
		if dirty {
			fmt.Print(" (DIRTY)")
		}
		fmt.Println()

	case "force":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "Uso: migrate force <version>\n")
			os.Exit(1)
		}
		version, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Versão inválida: %v\n", err)
			os.Exit(1)
		}
		err = runner.Force(ctx, version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao forçar versão: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Versão forçada para %d\n", version)

	default:
		fmt.Fprintf(os.Stderr, "Comando desconhecido: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Uso: migrate <comando> [args]

Comandos:
  up              Aplica todas as migrations pendentes
  down [N]        Reverte N migrations (padrão: todas)
  status          Mostra versão atual e migrations pendentes
  version         Mostra apenas a versão atual
  force <V>       Força versão V no banco (emergência)

Variáveis de ambiente:
  DATABASE_URL      postgres://user:pass@host:port/db?sslmode=disable
  MIGRATIONS_PATH   ./scripts/postgres/migrations

Exemplos:
  migrate up
  migrate down
  migrate down 1
  migrate status
  migrate force 3
`)
}
