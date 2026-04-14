package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/bubustack/bubu-sdk-go"

	cronimp "github.com/bubustack/cron-impulse/pkg/impulse"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := sdk.RunImpulse(ctx, cronimp.New()); err != nil {
		log.Fatalf("cron impulse failed: %v", err)
	}
}
