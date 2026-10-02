package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

// handlerMarco é o handler que registra se passou pelo middleware.
func handlerMarco(ok *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*ok = true
		w.WriteHeader(http.StatusOK)
	})
}

func capturaErro(t *testing.T, rr *httptest.ResponseRecorder) dto.ErrorResponse {
	t.Helper()
	var resp dto.ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é ErrorResponse: %v", err)
	}
	return resp
}

func TestAuthRejeitaSemHeader(t *testing.T) {
	ok := false
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	middleware.AuthMiddleware(handlerMarco(&ok)).ServeHTTP(rr, req)

	if ok {
		t.Error("handler não deveria rodar sem Authorization")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, quer 401", rr.Code)
	}
	resp := capturaErro(t, rr)
	if resp.Code != dto.ErrCodeUnauthorized {
		t.Errorf("code = %q", resp.Code)
	}
}

func TestAuthRejeitaEsquemaErrado(t *testing.T) {
	ok := false
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic abc123")

	middleware.AuthMiddleware(handlerMarco(&ok)).ServeHTTP(rr, req)

	if ok || rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava 401 sem rodar handler: ok=%v status=%d", ok, rr.Code)
	}
}

func TestAuthRejeitaTokenDesconhecido(t *testing.T) {
	ok := false
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token-aleatorio")

	middleware.AuthMiddleware(handlerMarco(&ok)).ServeHTTP(rr, req)

	if ok || rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava 401 sem rodar handler: ok=%v status=%d", ok, rr.Code)
	}
}

func TestAuthAceitaTokenDeDesenvolvimento(t *testing.T) {
	ok := false
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-provider1-player99")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok = true
		if got := middleware.GetProviderID(r); got != "provider1" {
			t.Errorf("providerId = %q", got)
		}
		if got := middleware.GetPlayerID(r); got != "player99" {
			t.Errorf("playerId = %q", got)
		}
		claims := middleware.GetAuthClaims(r)
		if claims == nil || claims.Subject != "provider1" || claims.Roles[0] != "player" {
			t.Errorf("claims = %+v", claims)
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware.AuthMiddleware(next).ServeHTTP(rr, req)

	if !ok {
		t.Error("handler deveria rodar com token válido")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, quer 200", rr.Code)
	}
}

func TestAuthRejeitaTokenCurto(t *testing.T) {
	// "test-provider1" não tem o segmento de jogador (precisam ser >= 3).
	ok := false
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer test-provider1")

	middleware.AuthMiddleware(handlerMarco(&ok)).ServeHTTP(rr, req)

	if ok || rr.Code != http.StatusUnauthorized {
		t.Errorf("esperava 401: ok=%v status=%d", ok, rr.Code)
	}
}

func TestIdempotencyExigeChaveNasMutacoes(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			ok := false
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/", nil)

			middleware.IdempotencyMiddleware(handlerMarco(&ok)).ServeHTTP(rr, req)

			if ok {
				t.Error("handler não deveria rodar sem chave de idempotência")
			}
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, quer 400", rr.Code)
			}
			resp := capturaErro(t, rr)
			if resp.Code != dto.ErrCodeInvalidInput {
				t.Errorf("code = %q", resp.Code)
			}
		})
	}
}

func TestIdempotencyAceitaComChave(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Idempotency-Key", "chave-1")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := middleware.GetIdempotencyKey(r); got != "chave-1" {
			t.Errorf("chave = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware.IdempotencyMiddleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, quer 200", rr.Code)
	}
}

func TestIdempotencyNaoCobraEmLeitura(t *testing.T) {
	ok := false
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	middleware.IdempotencyMiddleware(handlerMarco(&ok)).ServeHTTP(rr, req)

	if !ok || rr.Code != http.StatusOK {
		t.Errorf("leitura não pode exigir chave: ok=%v status=%d", ok, rr.Code)
	}
}

func TestChainAplicaEmOrdem(t *testing.T) {
	var ordem []string
	mw := func(nome string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ordem = append(ordem, nome)
				next.ServeHTTP(w, r)
			})
		}
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware.Chain(mw("a"), mw("b"), mw("c"))(final).ServeHTTP(rr, req)

	want := []string{"a", "b", "c"}
	if len(ordem) != 3 {
		t.Fatalf("ordem = %v", ordem)
	}
	for i := range want {
		if ordem[i] != want[i] {
			t.Errorf("ordem[%d] = %q, quer %q", i, ordem[i], want[i])
		}
	}
}
