package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/vinifuzetti/ai_identity/internal/spiffe"
)

func main() {
	socketPath := env("SPIRE_AGENT_SOCKET", "/opt/spire/sockets/agent.sock")
	audience := env("SVID_AUDIENCE", "empresa.com")

	ctx := context.Background()

	client, err := spiffe.NewClient(ctx, socketPath)
	if err != nil {
		log.Fatalf("falha ao conectar à Workload API: %v", err)
	}
	defer client.Close()

	svid, err := client.FetchJWTSVID(ctx, audience)
	if err != nil {
		log.Fatalf("falha ao buscar JWT SVID: %v", err)
	}

	fmt.Printf("SPIFFE ID : %s\n", svid.ID)
	fmt.Printf("Audience  : %v\n", svid.Audience)
	fmt.Printf("Expiry    : %s\n", svid.Expiry)
	fmt.Printf("JWT       : %s\n", svid.Marshal())
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
