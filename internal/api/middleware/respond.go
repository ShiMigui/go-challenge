package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/shimigui/go-challenge/internal/api/dto"
)

func RespondError(w http.ResponseWriter, r *http.Request, err *dto.DomainError) {
	status := err.StatusCode
	if status == 0 {
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
		Error:   err.Error(),
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
