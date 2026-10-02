package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/shimigui/go-challenge/internal/api/dto"
	"github.com/shimigui/go-challenge/internal/api/middleware"
)

type HealthChecker interface {
	CheckDatabase(ctx context.Context) error
	CheckMessaging(ctx context.Context) error
}

type HealthHandler struct {
	checker HealthChecker
}

func NewHealthHandler(checker HealthChecker) *HealthHandler {
	return &HealthHandler{checker: checker}
}

func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	middleware.RespondJSON(w, r, http.StatusOK, dto.HealthResponse{
		Status: "UP",
	})
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := make(map[string]string)

	if err := h.checker.CheckDatabase(ctx); err != nil {
		checks["database"] = "DOWN: " + err.Error()
	} else {
		checks["database"] = "UP"
	}

	if err := h.checker.CheckMessaging(ctx); err != nil {
		checks["messaging"] = "DOWN: " + err.Error()
	} else {
		checks["messaging"] = "UP"
	}

	status := "UP"
	for _, v := range checks {
		if v != "UP" {
			status = "DOWN"
			break
		}
	}

	code := http.StatusOK
	if status == "DOWN" {
		code = http.StatusServiceUnavailable
	}

	middleware.RespondJSON(w, r, code, dto.HealthResponse{
		Status: status,
		Checks: checks,
	})
}
