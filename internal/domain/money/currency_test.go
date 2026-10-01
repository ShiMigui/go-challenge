package money

import (
	"errors"
	"testing"
)

func TestValidAceitaTodaLista(t *testing.T) {
	for _, c := range All() {
		if err := Valid(c); err != nil {
			t.Errorf("Valid(%q) = %v, esperava nil", c, err)
		}
		if info, ok := Lookup(c); !ok || info.Code != c {
			t.Errorf("Lookup(%q) = %+v, %v", c, info, ok)
		}
	}
}

func TestValidRejeitaForaDaLista(t *testing.T) {
	// Códigos fora da lista de 160: fundos, metais, testes, retirados
	for _, c := range []Currency{"XBT", "ZWL", "CUC", "MRO", "STD", "VEF", "QQQ", "BRLX", "brl", "BRL ", "", "B", "BR", "XAU", "XAG", "XTS", "XXX", "BOV", "XUA", "CLF"} {
		if err := Valid(c); !errors.Is(err, ErrInvalidCurrency) {
			t.Errorf("Valid(%q) = %v, esperava ErrInvalidCurrency", c, err)
		}
	}
}

func TestCodigosConhecidosDaAplicacao(t *testing.T) {
	for _, c := range []Currency{BRL, USD, EUR} {
		if _, ok := Lookup(c); !ok {
			t.Errorf("%q deveria estar na lista", c)
		}
		if err := Valid(c); err != nil {
			t.Errorf("%q deveria ser válido: %v", c, err)
		}
	}
}

func TestAllOrdenadoESemRepeticao(t *testing.T) {
	lista := All()
	if len(lista) == 0 {
		t.Fatal("All() vazio")
	}
	for i := 1; i < len(lista); i++ {
		if lista[i-1] >= lista[i] {
			t.Fatalf("All() fora de ordem em %d: %s depois de %s", i, lista[i], lista[i-1])
		}
	}
	if len(lista) != 160 {
		t.Errorf("All() = %d códigos, esperava 160 (moedas circulantes)", len(lista))
	}
}

func TestAllDevolveCopia(t *testing.T) {
	lista := All()
	lista[0] = "ZZZ"
	if All()[0] == "ZZZ" {
		t.Error("All() devolveu o slice interno")
	}
}

func TestExpoenteOficial(t *testing.T) {
	casos := []struct {
		cur  Currency
		exp  int
		nome string
	}{
		{BRL, 2, "Real brasileiro"},
		{JPY, 0, "Iene sem subdivisao"},
		{KWD, 3, "Dinar do Kuwait em milesimos"},
		{KWD, 3, "Dinar do Kuwait em milesimos"},
	}
	for _, c := range casos {
		info, ok := Lookup(c.cur)
		if !ok {
			t.Fatalf("%q fora da lista", c.cur)
		}
		if info.MinorUnits != c.exp {
			t.Errorf("%q minorUnits = %d, esperava %d", c.cur, info.MinorUnits, c.exp)
		}
		if info.Name == "" {
			t.Errorf("%q sem nome", c.cur)
		}
		if info.Numeric < 1 {
			t.Errorf("%q numeric = %d", c.cur, info.Numeric)
		}
	}
}

func TestLookupDeCodigoInexistente(t *testing.T) {
	info, ok := Lookup("QQQ")
	if ok {
		t.Error("Lookup(\"QQQ\") deveria falhar")
	}
	if info != (CurrencyInfo{}) {
		t.Errorf("Lookup de código inexistente devolveu %+v, esperava zero", info)
	}
}

// O CHECK do banco é ^[A-Z]{3}$, mais permissivo que a lista oficial. O
// domínio é quem fecha a porta, então a lista precisa cobrir tudo que o
// banco aceitaria sem reclamar para nenhum valor ser rejeitado por acaso.
func TestListaCobreOBancoInteiro(t *testing.T) {
	for _, c := range All() {
		s := string(c)
		if len(s) != 3 || s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' || s[2] < 'A' || s[2] > 'Z' {
			t.Errorf("%q não casaria com o CHECK do banco", s)
		}
	}
}
