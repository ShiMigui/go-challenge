// Package config carrega a configuração da aplicação a partir do ambiente.
//
// Pacote folha: não importa nada além da biblioteca padrão, então pode
// ser usado por qualquer camada sem risco de ciclo de importação.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config concentra a configuração de todas as dependências da aplicação.
type Config struct {
	API API
	DB  DB
}

// API é a configuração do servidor HTTP.
type API struct {
	Port            string
	LogLevel        string
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
}

// DB é a configuração do banco de dados.
type DB struct {
	Host            string
	Port            string
	Name            string
	User            string
	Password        string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Load lê as variáveis de ambiente com os padrões do ambiente local.
//
// Valor numérico ou de duração malformado é erro: a aplicação não sobe
// com configuração quebrada às escondidas.
func Load() (Config, error) {
	cfg := Config{
		API: API{
			Port:     env("API_PORT", "8080"),
			LogLevel: env("SERVICE_LOG_LEVEL", "debug"),
		},
		DB: DB{
			Host:     env("DB_HOST", "postgres"),
			Port:     env("DB_PORT", "5432"),
			Name:     env("DB_NAME", "wagering"),
			User:     env("DB_USER", "wagering"),
			Password: env("DB_PASSWORD", "wagering"),
			SSLMode:  env("DB_SSLMODE", "disable"),
		},
	}

	var err error
	if cfg.API.StartupTimeout, err = durEnv("STARTUP_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.API.ShutdownTimeout, err = durEnv("SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DB.MaxOpenConns, err = intEnv("DB_MAX_OPEN_CONNS", 25); err != nil {
		return Config{}, err
	}
	if cfg.DB.MaxIdleConns, err = intEnv("DB_MAX_IDLE_CONNS", 5); err != nil {
		return Config{}, err
	}
	if cfg.DB.ConnMaxLifetime, err = durEnv("DB_CONN_MAX_LIFETIME", 5*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.DB.ConnMaxIdleTime, err = durEnv("DB_CONN_MAX_IDLE_TIME", time.Minute); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// DSN monta a string de conexão esperada pelo driver pgx.
func (d DB) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode)
}

// env devolve a variável de ambiente ou o padrão quando ausente.
func env(chave, padrao string) string {
	if v, ok := os.LookupEnv(chave); ok && v != "" {
		return v
	}
	return padrao
}

// intEnv devolve a variável numérica ou erro quando ela é inválida.
func intEnv(chave string, padrao int) (int, error) {
	v, ok := os.LookupEnv(chave)
	if !ok || v == "" {
		return padrao, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s=%q nao e inteiro", chave, v)
	}
	return n, nil
}

// durEnv devolve a duração da variável ou erro quando ela é inválida.
func durEnv(chave string, padrao time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(chave)
	if !ok || v == "" {
		return padrao, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s=%q nao e duracao", chave, v)
	}
	return d, nil
}
