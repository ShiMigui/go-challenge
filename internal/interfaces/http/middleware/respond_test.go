package middleware_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

func TestRespondJSONEscreveStatusECorpo(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	payload := map[string]any{"status": "UP"}
	middleware.RespondJSON(rr, req, http.StatusOK, payload)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, quer 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"status":"UP"}` {
		t.Errorf("corpo = %s", got)
	}
}

func TestRespondErrorUsaStatusECodigoDoDomainError(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	causa := errors.New("saldo insuficiente")
	middleware.RespondError(rr, req, dto.ErrInsufficientFundsError(causa))

	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, quer 409", rr.Code)
	}
	var body dto.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("corpo inválido: %v", err)
	}
	if body.Error != "saldo insuficiente" {
		t.Errorf("error = %q", body.Error)
	}
	if body.Code != dto.ErrCodeInsufficientFunds {
		t.Errorf("code = %q", body.Code)
	}
}

// Um DomainError com StatusCode zero (esquecido pelo chamador) deve cair em
// 500, nunca em 2xx.
func TestRespondErrorComStatusZeroViraInterno(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	middleware.RespondError(rr, req, &dto.DomainError{Err: errors.New("sem status")})

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, quer 500", rr.Code)
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Run("valido", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/",
			strings.NewReader(`{"playerId":"p1","initialBalance":{"amount":"25.00","currency":"BRL"}}`))
		var out dto.CreateWalletRequest
		if err := middleware.DecodeJSON(req, &out); err != nil {
			t.Fatalf("DecodeJSON: %v", err)
		}
		if out.PlayerID != "p1" || out.InitialBalance.Amount != "25.00" {
			t.Errorf("decodificado errado: %+v", out)
		}
	})

	t.Run("json invalido", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"playerId":`))
		var out dto.CreateWalletRequest
		if err := middleware.DecodeJSON(req, &out); err == nil {
			t.Fatal("JSON inválido deveria falhar")
		}
	})
}
