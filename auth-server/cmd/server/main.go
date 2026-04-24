package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/vinifuzetti/ai_identity/auth-server/internal/jwks"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/policy"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/tokenexchange"
)

func main() {
	bundlePath := env("SPIRE_BUNDLE_PATH", "/opt/spire/bundle/jwks.json")
	trustDomain := env("TRUST_DOMAIN", "empresa.com")
	idpKeyPath := env("IDP_PUBLIC_KEY", "/config/idp-public.pem")
	addr := env("ADDR", ":8080")

	idpKey, err := loadECPublicKey(idpKeyPath)
	if err != nil {
		log.Fatalf("falha ao carregar chave pública do IdP (%s): %v", idpKeyPath, err)
	}

	signingKey, err := jwks.NewSigningKey()
	if err != nil {
		log.Fatalf("falha ao gerar chave de assinatura: %v", err)
	}

	bundleSource, err := jwks.NewBundleFile(bundlePath, trustDomain)
	if err != nil {
		log.Fatalf("falha ao inicializar bundle source: %v", err)
	}

	pol := policy.New()
	handler := tokenexchange.NewHandler(idpKey, signingKey, bundleSource, pol)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", handler.ServeHTTP)
	mux.HandleFunc("GET /keys", handler.ServeJWKS)

	log.Printf("Authorization Server iniciando em %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func loadECPublicKey(path string) (*ecdsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("falha ao decodificar bloco PEM")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("não é uma chave EC")
	}
	return key, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
