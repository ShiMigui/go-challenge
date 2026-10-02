// Package config carrega a configuração da aplicação a partir do ambiente.
//
// Pacote folha: não importa nada além da biblioteca padrão, então pode
// ser usado por qualquer camada sem risco de ciclo de importação.
// A configuração é carregada automaticamente no init() e acessível via Get().
package config

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	cfg  Config
	once sync.Once
)

// Config concentra a configuração de todas as dependências da aplicação.
type Config struct {
	API    API
	DB     DB
	AppEnv string
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

// Get retorna a configuração carregada (thread-safe, carrega na primeira chamada).
func Get() Config {
	once.Do(func() {
		var err error
		cfg, err = load()
		if err != nil {
			panic(fmt.Errorf("config: falha ao carregar: %w", err))
		}
	})
	return cfg
}

// MustLoad carrega a configuração e panica se houver erro (para uso em init()).
func MustLoad() Config {
	var err error
	cfg, err = load()
	if err != nil {
		panic(fmt.Errorf("config: falha ao carregar: %w", err))
	}
	return cfg
}

// load faz o carregamento real das variáveis de ambiente.
func load() (Config, error) {
	c := Config{
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
		AppEnv: env("APP_ENV", "production"),
	}

	var err error
	if c.API.StartupTimeout, err = durEnv("STARTUP_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if c.API.ShutdownTimeout, err = durEnv("SHUTDOWN_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, err
	}
	if c.DB.MaxOpenConns, err = intEnv("DB_MAX_OPEN_CONNS", 25); err != nil {
		return Config{}, err
	}
	if c.DB.MaxIdleConns, err = intEnv("DB_MAX_IDLE_CONNS", 5); err != nil {
		return Config{}, err
	}
	if c.DB.ConnMaxLifetime, err = durEnv("DB_CONN_MAX_LIFETIME", 5*time.Minute); err != nil {
		return Config{}, err
	}
	if c.DB.ConnMaxIdleTime, err = durEnv("DB_CONN_MAX_IDLE_TIME", time.Minute); err != nil {
		return Config{}, err
	}
	return c, nil
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
