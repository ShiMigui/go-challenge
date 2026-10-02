package identifier

import "testing"

func TestNewGeraFormatoValido(t *testing.T) {
	primeiro, segundo := New(), New()
	if !IsValid(primeiro) {
		t.Fatalf("%q não é um identificador válido", primeiro)
	}
	if primeiro == segundo {
		t.Error("dois identificadores não podem coincidir")
	}
}

func TestNewMarcaVersao4EVariante(t *testing.T) {
	// Posição 14 é o nibble da versão e a 19 o da variante: é o que
	// distingue um id gerado de um id de outra origem.
	gerado := New()
	if gerado[14] != '4' {
		t.Errorf("versão deveria ser 4, veio %q", gerado[14])
	}
	if v := gerado[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
		t.Errorf("variante deveria ser RFC 4122, veio %q", v)
	}
}

func TestIsValid(t *testing.T) {
	validos := []string{
		"11111111-1111-1111-1111-111111111111",
		"aBcDeF01-2345-6789-abcd-ef0123456789",
	}
	for _, v := range validos {
		if !IsValid(v) {
			t.Errorf("%q deveria ser válido", v)
		}
	}
	invalidos := []string{
		"", "abc", "11111111-1111-1111-1111-11111111111",
		"11111111111111111111111111111111",
		"11111111_1111_1111_1111_111111111111",
		"1111111-1111-1111-1111-111111111111",
		"11111111-1111-1111-1111-11111111111g",
	}
	for _, v := range invalidos {
		if IsValid(v) {
			t.Errorf("%q não deveria ser válido", v)
		}
	}
}
