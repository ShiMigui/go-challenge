package middleware

import (
	"net/http"
)

// Chain composes multiple middleware functions into a single handler.
// Middlewares are applied in the order they are provided.
func Chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
