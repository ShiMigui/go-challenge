package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrationRunner gerencia aplicação e reversão de migrations PostgreSQL.
type MigrationRunner struct {
	db             *sql.DB
	migrationsPath string
}

// NewMigrationRunner cria um runner apontando para o diretório de migrations.
// Aceita *sql.DB padrão (database/sql) para compatibilidade com fx.Module.
func NewMigrationRunner(db *sql.DB, migrationsPath string) *MigrationRunner {
	return &MigrationRunner{
		db:             db,
		migrationsPath: migrationsPath,
	}
}

// NewMigrationRunnerFromPool cria um runner a partir de *pgxpool.Pool.
func NewMigrationRunnerFromPool(db interface{}, migrationsPath string) *MigrationRunner {
	if sqlDB, ok := db.(*sql.DB); ok {
		return NewMigrationRunner(sqlDB, migrationsPath)
	}
	// Se for *pgxpool.Pool, extrai o *sql.DB nativo
	// pgxpool não expõe *sql.DB diretamente, mas podemos usar a connection string
	return nil // Será tratado no módulo
}

// migrateInstance cria uma instância do migrate conectada ao banco.
func (r *MigrationRunner) migrateInstance() (*migrate.Migrate, error) {
	// golang-migrate precisa de connection string postgres
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		// Default local
		connStr = "postgres://wagering:wagering@localhost:5432/wagering?sslmode=disable"
	}

	sourceURL := "file://" + r.migrationsPath
	m, err := migrate.New(sourceURL, connStr)
	if err != nil {
		return nil, fmt.Errorf("create migrate instance: %w", err)
	}
	return m, nil
}

// Up aplica todas as migrations pendentes (up).
func (r *MigrationRunner) Up(ctx context.Context) error {
	m, err := r.migrateInstance()
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil // nenhuma migration pendente
		}
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Down reverte N migrations (down). Se steps <= 0, reverte tudo.
func (r *MigrationRunner) Down(ctx context.Context, steps int) error {
	m, err := r.migrateInstance()
	if err != nil {
		return err
	}
	defer m.Close()

	var migrateErr error
	if steps > 0 {
		migrateErr = m.Steps(-steps)
	} else {
		migrateErr = m.Down()
	}
	if migrateErr != nil {
		if errors.Is(migrateErr, migrate.ErrNoChange) {
			return nil
		}
		return fmt.Errorf("migrate down: %w", migrateErr)
	}
	return nil
}

// Force define a versão forçada no banco (uso em emergência).
func (r *MigrationRunner) Force(ctx context.Context, version int) error {
	m, err := r.migrateInstance()
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Force(version); err != nil {
		return fmt.Errorf("migrate force: %w", err)
	}
	return nil
}

// Version retorna a versão atual aplicada e se está suja.
func (r *MigrationRunner) Version(ctx context.Context) (uint, bool, error) {
	m, err := r.migrateInstance()
	if err != nil {
		return 0, false, err
	}
	defer m.Close()

	v, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil // nenhuma migration aplicada
		}
		return 0, false, fmt.Errorf("migrate version: %w", err)
	}
	return v, dirty, nil
}

// Status imprime status das migrations (aplicadas/pendentes).
func (r *MigrationRunner) Status(ctx context.Context) error {
	m, err := r.migrateInstance()
	if err != nil {
		return err
	}
	defer m.Close()

	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("migrate version: %w", err)
	}

	fmt.Printf("Current version: %d", v)
	if dirty {
		fmt.Printf(" (DIRTY)")
	}
	fmt.Println()

	// Listar migrations disponíveis
	entries, err := os.ReadDir(r.migrationsPath)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	fmt.Println("\nAvailable migrations:")
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".up.sql" {
			name := e.Name()[:len(e.Name())-len(".up.sql")]
			status := "pending"
			// Extrair número da versão do nome (ex: 000001_wallets)
			var ver uint
			fmt.Sscanf(name, "%d_", &ver)
			if ver <= v {
				status = "applied"
			}
			fmt.Printf("  %s [%s]\n", name, status)
		}
	}
	return nil
}

// RunMigrationsFromMain é o entrypoint para uso via CLI (main.go).
// Lê variáveis de ambiente e roda Up().
func RunMigrationsFromMain(ctx context.Context) error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://wagering:wagering@localhost:5432/wagering?sslmode=disable"
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer db.Close()

	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "./scripts/postgres/migrations"
	}

	runner := NewMigrationRunner(db, migrationsPath)
	return runner.Up(ctx)
}
