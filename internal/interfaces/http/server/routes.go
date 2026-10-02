package server

import (
	"net/http"

	"github.com/shimigui/go-challenge/internal/interfaces/http/handler"
	"github.com/shimigui/go-challenge/internal/interfaces/http/middleware"
)

func NewRouter(
	walletH *handler.WalletHandler,
	wageringH *handler.WageringHandler,
	healthH *handler.HealthHandler,
) http.Handler {
	mux := http.NewServeMux()

	// Public health checks
	mux.HandleFunc("GET /health/live", healthH.Live)
	mux.HandleFunc("GET /health/ready", healthH.Ready)

	protected := middleware.Chain(
		middleware.AuthMiddleware,
		middleware.IdempotencyMiddleware,
	)
	// Wallet routes
	mux.Handle("POST /wallets", protected(http.HandlerFunc(walletH.CreateWallet)))
	mux.Handle("GET /wallets/{walletId}", protected(http.HandlerFunc(walletH.GetWallet)))
	mux.Handle("GET /wallets/{walletId}/ledger", protected(http.HandlerFunc(walletH.GetLedger)))
	mux.Handle("POST /wallets/{walletId}/reconciliation", protected(http.HandlerFunc(walletH.Reconcile)))

	// Wagering routes
	mux.Handle("POST /wagering/transactions", protected(http.HandlerFunc(wageringH.SubmitTransaction)))
	mux.Handle("GET /wagering/transactions/{transactionId}", protected(http.HandlerFunc(wageringH.GetTransaction)))
	mux.Handle("GET /providers/{providerId}/wagering/transactions/{externalTransactionId}", protected(http.HandlerFunc(wageringH.GetTransactionByExternal)))

	return mux
}
