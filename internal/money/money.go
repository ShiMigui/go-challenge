// Package money representa valores monetários exatos.
//
// Dinheiro nunca passa por float32 ou float64. O valor é um int64 em
// unidades mínimas (centavos) e a moeda é um código ISO 4217, então a
// aritmética em Go e no banco é a mesma operação inteira.
package money

import (
	"errors"
	"fmt"
	"strings"
)

// Currency é um código ISO 4217 de três letras.
type Currency string

const (
	BRL Currency = "BRL"
	USD Currency = "USD"
	EUR Currency = "EUR"
)

var (
	// ErrCurrencyMismatch é devolvido em operações entre moedas diferentes.
	ErrCurrencyMismatch = errors.New("moedas incompativeis")
	// ErrNegativeAmount é devolvido quando um valor negativo entra onde não é aceito.
	ErrNegativeAmount = errors.New("valor negativo nao permitido")
	// ErrInvalidAmount é devolvido para valores malformados ou com overflow.
	ErrInvalidAmount = errors.New("valor monetario invalido")
	// ErrInvalidCurrency é devolvido para códigos que não são ISO 4217.
	ErrInvalidCurrency = errors.New("codigo de moeda invalido")
)

// scale é o número de casas decimais com que a moeda é operada.
const scale = 2

// Money é um valor exato em unidades mínimas, sempre com a moeda.
//
// O valor zero de Money não é um valor monetário: use New ou Parse.
type Money struct {
	amount   int64
	currency Currency
}

// New cria um Money a partir das unidades mínimas.
func New(amount int64, currency Currency) (Money, error) {
	if err := validCurrency(currency); err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: currency}, nil
}

// Zero devolve zero na moeda informada.
func Zero(currency Currency) Money {
	m, _ := New(0, currency)
	return m
}

// FromMajor cria um Money a partir de unidades inteiras e centavos.
func FromMajor(major, minor int64, currency Currency) (Money, error) {
	if err := validCurrency(currency); err != nil {
		return Money{}, err
	}
	if minor < 0 || minor >= 100 {
		return Money{}, fmt.Errorf("%w: centavos fora de 0..99: %d", ErrInvalidAmount, minor)
	}
	return Money{amount: major*100 + minor, currency: currency}, nil
}

// Parse converte "25.00" em Money. Rejeita vazio, NaN, Infinity, notação
// científica, mais de duas casas e valores negativos.
func Parse(value string, currency Currency) (Money, error) {
	if err := validCurrency(currency); err != nil {
		return Money{}, err
	}
	raw := strings.TrimSpace(value)
	if raw == "" {
		return Money{}, fmt.Errorf("%w: vazio", ErrInvalidAmount)
	}
	if strings.ContainsAny(raw, "eE") {
		return Money{}, fmt.Errorf("%w: notacao cientifica: %q", ErrInvalidAmount, value)
	}
	// '+' e '-' só são rejeitados aqui pela forma; o sinal é tratado abaixo.
	neg := strings.HasPrefix(raw, "-")
	if neg || strings.HasPrefix(raw, "+") {
		raw = raw[1:]
	}
	if strings.Contains(raw, ",") {
		return Money{}, fmt.Errorf("%w: virgula nao suportada: %q", ErrInvalidAmount, value)
	}
	parts := strings.Split(raw, ".")
	if len(parts) > 2 {
		return Money{}, fmt.Errorf("%w: mais de um separador decimal: %q", ErrInvalidAmount, value)
	}
	if parts[0] == "" {
		return Money{}, fmt.Errorf("%w: sem parte inteira: %q", ErrInvalidAmount, value)
	}
	major, ok := parseDigits(parts[0])
	if !ok {
		return Money{}, fmt.Errorf("%w: parte inteira nao numerica: %q", ErrInvalidAmount, value)
	}
	var minor int64
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > scale {
			return Money{}, fmt.Errorf("%w: escala invalida: %q", ErrInvalidAmount, value)
		}
		minor, ok = parseDigits(parts[1])
		if !ok {
			return Money{}, fmt.Errorf("%w: centavos nao numericos: %q", ErrInvalidAmount, value)
		}
		// "25.5" significa 25 reais e 50 centavos.
		for i := int64(len(parts[1])); i < scale; i++ {
			minor *= 10
		}
	}
	if !multiplyFits(major, 100) {
		return Money{}, fmt.Errorf("%w: overflow em %q", ErrInvalidAmount, value)
	}
	amount := major*100 + minor
	if neg {
		amount = -amount
	}
	return Money{amount: amount, currency: currency}, nil
}

// MustParse é Parse para valores conhecidos em tempo de compilação.
func MustParse(value string, currency Currency) Money {
	m, err := Parse(value, currency)
	if err != nil {
		panic(err)
	}
	return m
}

// Amount devolve as unidades mínimas.
func (m Money) Amount() int64 { return m.amount }

// Currency devolve a moeda.
func (m Money) Currency() Currency { return m.currency }

// IsZero informa se o valor é zero.
func (m Money) IsZero() bool { return m.amount == 0 }

// IsPositive informa se o valor é maior que zero.
func (m Money) IsPositive() bool { return m.amount > 0 }

// IsNegative informa se o valor é menor que zero.
func (m Money) IsNegative() bool { return m.amount < 0 }

// Add soma dois valores. Exige moedas iguais.
func (m Money) Add(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s + %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	sum := m.amount + other.amount
	// Overflow só é possível quando os dois somandos têm o mesmo sinal.
	if (sum > m.amount) != (m.amount > 0) && (m.amount != 0) {
		return Money{}, fmt.Errorf("%w: overflow na soma", ErrInvalidAmount)
	}
	return Money{amount: sum, currency: m.currency}, nil
}

// Sub subtrai dois valores. Exige moedas iguais.
func (m Money) Sub(other Money) (Money, error) {
	if m.currency != other.currency {
		return Money{}, fmt.Errorf("%w: %s - %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	// Negativo menos positivo estoura quando o positivo é grande demais.
	if m.amount < 0 && other.amount > 0 && m.amount < minInt64+other.amount {
		return Money{}, fmt.Errorf("%w: overflow na subtracao", ErrInvalidAmount)
	}
	// Positivo menos negativo estoura quando o negativo é grande demais.
	if m.amount > 0 && other.amount < 0 && m.amount > maxInt64+other.amount {
		return Money{}, fmt.Errorf("%w: overflow na subtracao", ErrInvalidAmount)
	}
	return Money{amount: m.amount - other.amount, currency: m.currency}, nil
}

// Neg devolve o valor com sinal invertido. MinInt64 não pode ser invertido.
func (m Money) Neg() (Money, error) {
	if m.amount == minInt64 {
		return Money{}, fmt.Errorf("%w: overflow na negacao", ErrInvalidAmount)
	}
	return Money{amount: -m.amount, currency: m.currency}, nil
}

// Abs devolve o valor absoluto.
func (m Money) Abs() (Money, error) {
	if !m.IsNegative() {
		return m, nil
	}
	return m.Neg()
}

// Equal informa se os valores e as moedas são iguais.
func (m Money) Equal(other Money) bool {
	return m.amount == other.amount && m.currency == other.currency
}

// Cmp compara dois valores: -1, 0 ou 1. Exige moedas iguais.
func (m Money) Cmp(other Money) (int, error) {
	if m.currency != other.currency {
		return 0, fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.currency, other.currency)
	}
	switch {
	case m.amount < other.amount:
		return -1, nil
	case m.amount > other.amount:
		return 1, nil
	default:
		return 0, nil
	}
}

// GreaterThan informa se m é maior que other. Exige moedas iguais.
func (m Money) GreaterThan(other Money) (bool, error) {
	c, err := m.Cmp(other)
	return c > 0, err
}

// GreaterThanOrEqual informa se m é maior ou igual a other.
func (m Money) GreaterThanOrEqual(other Money) (bool, error) {
	c, err := m.Cmp(other)
	return c >= 0, err
}

// String devolve "25.00". Valores negativos levam o sinal.
func (m Money) String() string {
	amount := m.amount
	sign := ""
	if amount < 0 {
		sign = "-"
		// -MinInt64 não é representável em int64 positivo.
		if amount == minInt64 {
			return sign + "92233720368547758.08"
		}
		amount = -amount
	}
	return fmt.Sprintf("%s%d.%02d", sign, amount/100, amount%100)
}

// MarshalJSON serializa como string decimal, não como número.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.String() + `"`), nil
}

// UnmarshalJSON aceita apenas string decimal.
func (m *Money) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "null" || s == "" {
		return fmt.Errorf("%w: json nulo ou vazio", ErrInvalidAmount)
	}
	if m.currency == "" {
		return fmt.Errorf("%w: moeda nao definida antes do parse", ErrInvalidAmount)
	}
	parsed, err := Parse(s, m.currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

const (
	minInt64 = -1 << 63
	maxInt64 = 1<<63 - 1
)

// validCurrency exige três letras maiúsculas ASCII.
func validCurrency(c Currency) error {
	if len(c) != 3 {
		return fmt.Errorf("%w: %q", ErrInvalidCurrency, string(c))
	}
	for i := 0; i < 3; i++ {
		if c[i] < 'A' || c[i] > 'Z' {
			return fmt.Errorf("%w: %q", ErrInvalidCurrency, string(c))
		}
	}
	return nil
}

func parseDigits(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var out int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		d := int64(s[i] - '0')
		if !multiplyFits(out, 10) || out*10 > (1<<63-1)-d {
			return 0, false
		}
		out = out*10 + d
	}
	return out, true
}

func multiplyFits(v, factor int64) bool {
	return v <= (1<<63-1)/factor
}
