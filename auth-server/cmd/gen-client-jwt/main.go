// gen-client-jwt gera um JWT de cliente simulando um token emitido pelo IdP corporativo.
// Usado apenas para testes da PoC — requer que 'make gen-idp-keys' tenha sido executado.
package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func main() {
	keyPath := env("IDP_PRIVATE_KEY", "config/idp-private.pem")

	key, err := loadECPrivateKey(keyPath)
	if err != nil {
		log.Fatalf("falha ao carregar chave privada do IdP (%s): %v\nExecute 'make gen-idp-keys' primeiro.", keyPath, err)
	}

	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		log.Fatalf("falha ao criar signer: %v", err)
	}

	now := time.Now()
	claims := jwt.Claims{
		Subject:  "user-8f3a2c",
		Issuer:   "https://idp.empresa.com",
		Audience: jwt.Audience{"https://auth.empresa.com"},
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(1 * time.Hour)),
	}

	token, err := jwt.Signed(sig).Claims(claims).Serialize()
	if err != nil {
		log.Fatalf("falha ao serializar JWT: %v", err)
	}

	fmt.Println(token)
}

func loadECPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("falha ao decodificar bloco PEM")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
