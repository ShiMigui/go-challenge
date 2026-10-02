// Package identifier gera as identidades dos agregados.
//
// Todo agregado começa com um id gerado aqui e nunca com um valor
// escolhido pelo chamador: é o que impede duas operações de nascer com
// a mesma identidade.
package identifier

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New devolve um identificador único no formato 8-4-4-4-12.
//
// O formato é o mesmo exigido pelo schema, então o valor gerado aqui vai
// para o banco sem tradução.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Sem entropia não há identidade: seguir com bytes fixos criaria
		// colisões silenciosas, que é pior que uma recusa.
		panic(fmt.Sprintf("identifier: sem fonte de aleatoriedade: %v", err))
	}
	// Versão 4 e variante RFC 4122, para o valor ser reconhecível como
	// gerado e não confundido com um id de outra origem.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// IsValid confere o formato 8-4-4-4-12 em hexadecimal.
func IsValid(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s[i] != '-' {
				return false
			}
			continue
		}
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
