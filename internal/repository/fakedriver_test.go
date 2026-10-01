package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"testing"
)

// Driver falso para exercitar os repositórios sem Postgres.
//
// Existe por dois motivos: o go.mod fica sem driver de produção, e os
// testes verificam o que o repositório faz com o banco (SQL enviado,
// argumentos, colunas lidas, erros traduzidos) sem precisar de container.
// As garantias do banco em si são verificadas pelo make db-test.

const driverFalso = "repository-falso"

// resposta é o que o driver devolve para um statement.
type resposta struct {
	colunas  []string
	linhas   [][]driver.Value
	erro     error
	afetadas int64
}

type consulta struct {
	query string
	args  []driver.NamedValue
}

var (
	muRespostas sync.Mutex
	respostas   = map[string]func(consulta) resposta{}
)

// registra o behaviour do driver falso para um DSN e devolve o *sql.DB.
func abrirFalso(t *testing.T, dsn string, fn func(consulta) resposta) *sql.DB {
	t.Helper()
	muRespostas.Lock()
	respostas[dsn] = fn
	muRespostas.Unlock()
	t.Cleanup(func() {
		muRespostas.Lock()
		delete(respostas, dsn)
		muRespostas.Unlock()
	})
	db, err := sql.Open(driverFalso, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

type drvFalso struct{}

func (drvFalso) Open(dsn string) (driver.Conn, error) {
	muRespostas.Lock()
	fn, ok := respostas[dsn]
	muRespostas.Unlock()
	if !ok {
		return nil, fmt.Errorf("dsn %q sem resposta registrada", dsn)
	}
	return &connFalso{fn: fn}, nil
}

type connFalso struct{ fn func(consulta) resposta }

func (c *connFalso) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("driver falso não usa prepare")
}
func (c *connFalso) Close() error              { return nil }
func (c *connFalso) Begin() (driver.Tx, error) { return txFalso{}, nil }

// argsVistos converte os argumentos para a forma que o teste inspeciona.
func argsVistos(args []driver.NamedValue) []any {
	saida := make([]any, len(args))
	for i, a := range args {
		saida[i] = a.Value
	}
	return saida
}

func (c *connFalso) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	r := c.fn(consulta{query: query, args: args})
	if r.erro != nil {
		return nil, r.erro
	}
	return &rowsFalso{colunas: r.colunas, linhas: r.linhas}, nil
}

func (c *connFalso) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	r := c.fn(consulta{query: query, args: args})
	if r.erro != nil {
		return nil, r.erro
	}
	return resultFalso{afetadas: r.afetadas}, nil
}

type txFalso struct{}

func (txFalso) Commit() error   { return nil }
func (txFalso) Rollback() error { return nil }

type resultFalso struct{ afetadas int64 }

func (r resultFalso) LastInsertId() (int64, error) { return 0, nil }
func (r resultFalso) RowsAffected() (int64, error) { return r.afetadas, nil }

type rowsFalso struct {
	colunas []string
	linhas  [][]driver.Value
	pos     int
}

func (r *rowsFalso) Columns() []string { return r.colunas }
func (r *rowsFalso) Close() error      { return nil }

func (r *rowsFalso) Next(dest []driver.Value) error {
	if r.pos >= len(r.linhas) {
		// io.EOF é o sinal de fim de linhas para database/sql. Outro
		// erro aqui seria reportado como falha de query.
		return io.EOF
	}
	linha := r.linhas[r.pos]
	r.pos++
	for i := range dest {
		if i < len(linha) {
			dest[i] = linha[i]
		} else {
			dest[i] = nil
		}
	}
	return nil
}

func init() { sql.Register(driverFalso, drvFalso{}) }
