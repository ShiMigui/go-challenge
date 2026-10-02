package service

import (
	"github.com/shimigui/go-challenge/internal/domain/ledger"
	"github.com/shimigui/go-challenge/internal/domain/money"
)

// sumEntries soma os lançamentos com sinal, reconstruindo o saldo do
// ledger para reconciliação.
func sumEntries(entries []*ledger.Entry) (money.Money, error) {
	var total money.Money
	for _, e := range entries {
		signed, err := e.SignedAmount()
		if err != nil {
			return money.Money{}, err
		}
		total, err = total.Add(signed)
		if err != nil {
			return money.Money{}, err
		}
	}
	return total, nil
}
