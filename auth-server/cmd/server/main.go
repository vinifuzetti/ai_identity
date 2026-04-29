package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/vinifuzetti/ai_identity/auth-server/internal/audit"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/jwks"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/policy"
	"github.com/vinifuzetti/ai_identity/auth-server/internal/tokenexchange"
)

func main() {
	audit.Init("auth-server")

	bundlePath := env("SPIRE_BUNDLE_PATH", "/opt/spire/bundle/jwks.json")
	trustDomain := env("TRUST_DOMAIN", "empresa.com")
	idpKeyPath := env("IDP_PUBLIC_KEY", "/config/idp-public.pem")
	addr := env("ADDR", ":8080")

	idpKey, err := loadECPublicKey(idpKeyPath)
	if err != nil {
		slog.Error("falha ao carregar chave pública do IdP", "path", idpKeyPath, "error", err)
		os.Exit(1)
	}

	signingKey, err := jwks.NewSigningKey()
	if err != nil {
		slog.Error("falha ao gerar chave de assinatura", "error", err)
		os.Exit(1)
	}

	bundleSource, err := jwks.NewBundleFile(bundlePath, trustDomain)
	if err != nil {
		slog.Error("falha ao inicializar bundle source", "error", err)
		os.Exit(1)
	}

	pol := policy.New()
	handler := tokenexchange.NewHandler(idpKey, signingKey, bundleSource, pol)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", handler.ServeHTTP)
	mux.HandleFunc("GET /keys", handler.ServeJWKS)

	slog.Info("Authorization Server iniciando", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("servidor encerrado", "error", err)
		os.Exit(1)
	}
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
