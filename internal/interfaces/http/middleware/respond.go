package middleware

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/shimigui/go-challenge/internal/interfaces/http/dto"
)

var appEnv = os.Getenv("APP_ENV")

// SetAppEnv define o ambiente de execução para controle de mensagens de erro.
// Deve ser chamado no início da aplicação (bootstrap).
func SetAppEnv(env string) {
	appEnv = env
}

func isDevelopment() bool {
	return appEnv == "development"
}

func RespondError(w http.ResponseWriter, r *http.Request, err *dto.DomainError) {
	status := err.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}

	// Em production, ocultamos detalhes internos de erros não mapeados
	message := err.Error()
	if !isDevelopment() && err.Code == dto.ErrCodeInternal {
		message = "erro interno do servidor"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
		Error:   message,
		Code:    err.Code,
		Details: "",
	})
}

func RespondJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func DecodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}
