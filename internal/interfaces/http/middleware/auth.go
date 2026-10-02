package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
)

type contextKey string

const (
	ctxKeyAuthClaims     = contextKey("auth_claims")
	ctxKeyProviderID     = contextKey("provider_id")
	ctxKeyPlayerID       = contextKey("player_id")
	ctxKeyIdempotencyKey = contextKey("idempotency_key")
)

type AuthClaims struct {
	Subject    string
	ProviderID string
	PlayerID   string
	Roles      []string
}

// AuthMiddleware valida o token Bearer e extrai as claims.
// A validação real fica no gateway/IdP; aqui extraímos o que o proxy já
// confirmou.
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			RespondError(w, r, dto.ErrUnauthorized(errors.New("missing authorization header")))
			return
		}
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			RespondError(w, r, dto.ErrUnauthorized(errors.New("invalid authorization format")))
			return
		}
		token := parts[1]

		// TODO: validar assinatura, exp, iss e aud do token.
		// Por ora as claims vêm de um formato conhecido de token local.
		claims := parseClaims(token)
		if claims == nil {
			RespondError(w, r, dto.ErrUnauthorized(errors.New("invalid token")))
			return
		}

		ctx := context.WithValue(r.Context(), ctxKeyAuthClaims, claims)
		ctx = context.WithValue(ctx, ctxKeyProviderID, claims.ProviderID)
		ctx = context.WithValue(ctx, ctxKeyPlayerID, claims.PlayerID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func parseClaims(token string) *AuthClaims {
	// Stub para desenvolvimento: sem validação criptográfica real.
	// A integração com o IdP substitui este trecho.
	if strings.HasPrefix(token, "test-") {
		parts := strings.Split(token, "-")
		if len(parts) >= 3 {
			return &AuthClaims{
				Subject:    parts[1],
				ProviderID: parts[1],
				PlayerID:   parts[2],
				Roles:      []string{"player"},
			}
		}
	}
	return nil
}

func GetAuthClaims(r *http.Request) *AuthClaims {
	if v := r.Context().Value(ctxKeyAuthClaims); v != nil {
		return v.(*AuthClaims)
	}
	return nil
}

func GetProviderID(r *http.Request) string {
	if v := r.Context().Value(ctxKeyProviderID); v != nil {
		return v.(string)
	}
	return ""
}

func GetPlayerID(r *http.Request) string {
	if v := r.Context().Value(ctxKeyPlayerID); v != nil {
		return v.(string)
	}
	return ""
}

// IdempotencyMiddleware cobra o header de idempotência nas mutações.
func IdempotencyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" || r.Method == "DELETE" {
			key := r.Header.Get("Idempotency-Key")
			if key == "" {
				RespondError(w, r, dto.ErrInvalidInput(errors.New("missing Idempotency-Key header")))
				return
			}
			// Fica no contexto para o handler usar depois.
			ctx := context.WithValue(r.Context(), ctxKeyIdempotencyKey, key)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

func GetIdempotencyKey(r *http.Request) string {
	if v := r.Context().Value(ctxKeyIdempotencyKey); v != nil {
		return v.(string)
	}
	return ""
}
