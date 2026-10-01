package money

import (
	"fmt"
	"slices"
)

// Currency é um código ISO 4217 de três letras (apenas moedas nacionais circulantes).
type Currency string

// currencyData é o registro de uma moeda circulante.
type currencyData struct {
	name       string
	minorUnits int
	numeric    int
}

// Lookup devolve os dados da moeda e informa se o código existe na lista
// de moedas circulantes (160 códigos).
func Lookup(c Currency) (CurrencyInfo, bool) {
	d, ok := iso4217[c]
	if !ok {
		return CurrencyInfo{}, false
	}
	return CurrencyInfo{
		Code:       c,
		Name:       d.name,
		MinorUnits: d.minorUnits,
		Numeric:    d.numeric,
	}, true
}

// CurrencyInfo são os dados oficiais de uma moeda circulante.
type CurrencyInfo struct {
	Code       Currency
	Name       string
	MinorUnits int
	Numeric    int
}

// All devolve as 160 moedas nacionais circulantes, em ordem alfabética.
func All() []Currency {
	return append([]Currency(nil), todas...)
}

// Valid devolve erro se o código não for moeda nacional circulante.
//
// A lista contém apenas as 160 moedas circulantes; fundos, metais, códigos
// de teste e códigos retirados (XBT, ZWL, CUC...) não estão aqui.
func Valid(c Currency) error {
	if _, ok := iso4217[c]; !ok {
		return fmt.Errorf("%w: %q", ErrInvalidCurrency, string(c))
	}
	return nil
}

var todas []Currency

func init() {
	todas = make([]Currency, 0, len(iso4217))
	for c := range iso4217 {
		todas = append(todas, c)
	}
	slices.Sort(todas)
}
