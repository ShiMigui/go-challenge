package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
	"github.com/shimigui/go-challenge/internal/interfaces/http/handler"
)

// healthCheckerStub verifica dependências com comportamento configurável.
type healthCheckerStub struct {
	db        func(ctx context.Context) error
	messaging func(ctx context.Context) error
}

func (s *healthCheckerStub) CheckDatabase(ctx context.Context) error {
	if s.db == nil {
		return errors.New("stub db não configurado")
	}
	return s.db(ctx)
}

func (s *healthCheckerStub) CheckMessaging(ctx context.Context) error {
	if s.messaging == nil {
		return errors.New("stub messaging não configurado")
	}
	return s.messaging(ctx)
}

func decodeHealth(t *testing.T, rr *httptest.ResponseRecorder) dto.HealthResponse {
	t.Helper()
	var resp dto.HealthResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("corpo inválido: %v", err)
	}
	return resp
}

func TestLiveSempreUp(t *testing.T) {
	h := handler.NewHealthHandler(&healthCheckerStub{})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)

	h.Live(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	if resp := decodeHealth(t, rr); resp.Status != "UP" {
		t.Errorf("status = %q", resp.Status)
	}
}

func TestReadyTudoUp(t *testing.T) {
	h := handler.NewHealthHandler(&healthCheckerStub{
		db:        func(ctx context.Context) error { return nil },
		messaging: func(ctx context.Context) error { return nil },
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

	h.Ready(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200", rr.Code)
	}
	resp := decodeHealth(t, rr)
	if resp.Status != "UP" {
		t.Errorf("status = %q", resp.Status)
	}
	if resp.Checks["database"] != "UP" || resp.Checks["messaging"] != "UP" {
		t.Errorf("checks = %+v", resp.Checks)
	}
}

func TestReadyMensageriaDown(t *testing.T) {
	h := handler.NewHealthHandler(&healthCheckerStub{
		db:        func(ctx context.Context) error { return nil },
		messaging: func(ctx context.Context) error { return errors.New("fila indisponivel") },
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quer 503", rr.Code)
	}
	resp := decodeHealth(t, rr)
	if resp.Status != "DOWN" {
		t.Errorf("status = %q, quer DOWN", resp.Status)
	}
	if resp.Checks["messaging"] != "DOWN: fila indisponivel" {
		t.Errorf("messaging = %q", resp.Checks["messaging"])
	}
	if resp.Checks["database"] != "UP" {
		t.Errorf("database = %q", resp.Checks["database"])
	}
}

func TestReadyBancoDown(t *testing.T) {
	h := handler.NewHealthHandler(&healthCheckerStub{
		db:        func(ctx context.Context) error { return errors.New("sem conexao") },
		messaging: func(ctx context.Context) error { return nil },
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)

	h.Ready(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quer 503", rr.Code)
	}
	resp := decodeHealth(t, rr)
	if resp.Checks["database"] != "DOWN: sem conexao" {
		t.Errorf("database = %q", resp.Checks["database"])
	}
}
