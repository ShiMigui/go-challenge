package event

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shimigui/go-challenge/internal/domain/identifier"
)

var (
	eventoID   = "11111111-1111-4111-8111-111111111111"
	agregadoID = "22222222-2222-4222-8222-222222222222"
	agora      = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
)

func TestNewEventValido(t *testing.T) {
	payload := []byte(`{"walletId":"` + agregadoID + `","balance":"75.00"}`)

	evt, err := NewEvent(eventoID, "wallet", agregadoID, "WalletBalanceChanged", 1, payload, agora)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}

	if evt.ID() != eventoID || evt.AggregateType() != "wallet" || evt.AggregateID() != agregadoID {
		t.Errorf("identidade = %s / %s / %s", evt.ID(), evt.AggregateType(), evt.AggregateID())
	}
	if evt.EventType() != "WalletBalanceChanged" || evt.Version() != 1 {
		t.Errorf("tipo/versão = %s / %d", evt.EventType(), evt.Version())
	}
	if string(evt.Payload()) != string(payload) {
		t.Errorf("payload = %s", evt.Payload())
	}
	if !evt.OccurredAt().Equal(agora) {
		t.Errorf("occurredAt = %v", evt.OccurredAt())
	}
	if evt.CorrelationID() != "" || evt.CausationID() != "" {
		t.Error("evento novo não pode ter correlação/causa")
	}
	if evt.Attempts() != 0 {
		t.Errorf("attempts = %d, quer 0", evt.Attempts())
	}
}

// O evento exige identidade válida: quem publica sem UUID é recusado na
// raiz, antes de tentar gravar no banco.
func TestNewEventRejeitaIdsInvalidos(t *testing.T) {
	payload := []byte(`{"a":1}`)

	for _, c := range []struct {
		name string
		id   string
	}{
		{"id vazio", ""},
		{"id nao-uuid", "evento-1"},
		{"agregado vazio", agregadoID},
	} {
		t.Run(c.name, func(t *testing.T) {
			var err error
			if c.name == "agregado vazio" {
				_, err = NewEvent(eventoID, "wallet", "", "Tipo", 1, payload, agora)
			} else {
				_, err = NewEvent(c.id, "wallet", agregadoID, "Tipo", 1, payload, agora)
			}
			if !errors.Is(err, identifier.ErrInvalidID) {
				t.Fatalf("esperava ErrInvalidID, veio %v", err)
			}
		})
	}
}

func TestNewEventRejeitaVersaoInvalida(t *testing.T) {
	_, err := NewEvent(eventoID, "wallet", agregadoID, "Tipo", 0, []byte(`{"a":1}`), agora)
	if err == nil {
		t.Fatal("versão 0 deveria ser recusada")
	}
}

// O payload precisa ser um objeto JSON: é a constraint do banco
// (jsonb_typeof = 'object'). Rejeitar aqui devolve erro de entrada, não de
// constraint.
func TestNewEventRejeitaPayloadNaoObjeto(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"não é JSON", []byte(`{incompleto`)},
		{"array", []byte(`[1,2,3]`)},
		{"string", []byte(`"texto"`)},
		{"número", []byte(`42`)},
		{"null", []byte(`null`)},
		{"string vazia", []byte(`""`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewEvent(eventoID, "wallet", agregadoID, "Tipo", 1, c.payload, agora)
			if err == nil {
				t.Fatalf("payload %s deveria ser recusado", c.payload)
			}
			if !strings.Contains(err.Error(), "payload") {
				t.Errorf("erro não fala do payload: %v", err)
			}
		})
	}
}

func TestNewEventAceitaPayloadObjetoVazio(t *testing.T) {
	// Objeto vazio é objeto válido; null é que não é.
	evt, err := NewEvent(eventoID, "wallet", agregadoID, "Tipo", 1, []byte(`{}`), agora)
	if err != nil {
		t.Fatalf("objeto vazio deveria passar: %v", err)
	}
	if len(evt.Payload()) == 0 {
		t.Error("payload vazio não preservado")
	}
}

// Rehydrate recria a partir do banco sem revalidar: dados legítimos
// gravados em versões passadas não podem ser recusados na leitura.
func TestRehydrateNaoRevalida(t *testing.T) {
	// Mesmo payload "errado" sob as regras atuais (string) é aceito na
	// reidratação, porque o banco já garantiu as invariantes na escrita.
	payload := []byte(`"nao-e-objeto"`)
	evt := Rehydrate(eventoID, "wallet", agregadoID, "Tipo", 1,
		"corr-1", "causa-1", payload, agora, 3)

	if evt.ID() != eventoID || evt.EventType() != "Tipo" {
		t.Errorf("identidade = %s/%s", evt.ID(), evt.EventType())
	}
	if evt.CorrelationID() != "corr-1" || evt.CausationID() != "causa-1" {
		t.Errorf("correlação/causa = %q/%q", evt.CorrelationID(), evt.CausationID())
	}
	if evt.Attempts() != 3 {
		t.Errorf("attempts = %d, quer 3", evt.Attempts())
	}
	if string(evt.Payload()) != string(payload) {
		t.Error("payload não preservado na reidratação")
	}
}

func TestWithCorrelationNaoMutaOriginal(t *testing.T) {
	original, err := NewEvent(eventoID, "wallet", agregadoID, "Tipo", 1, []byte(`{"a":1}`), agora)
	if err != nil {
		t.Fatal(err)
	}

	comCorr := original.WithCorrelation("corr-1", "causa-1")

	if comCorr.CorrelationID() != "corr-1" || comCorr.CausationID() != "causa-1" {
		t.Errorf("cópia = %q/%q", comCorr.CorrelationID(), comCorr.CausationID())
	}
	if original.CorrelationID() != "" || original.CausationID() != "" {
		t.Error("WithCorrelation mutou o original")
	}
	if comCorr.ID() != original.ID() || comCorr.EventType() != original.EventType() {
		t.Error("cópia perdeu a identidade")
	}
}

func TestValidaPayloadObjeto(t *testing.T) {
	if err := validaPayloadObjeto([]byte(`{"a":{"b":[1,2]}}`)); err != nil {
		t.Errorf("objeto aninhado deveria passar: %v", err)
	}
	if err := validaPayloadObjeto([]byte(`{"x":null}`)); err != nil {
		t.Errorf("null como valor é válido: %v", err)
	}
}
