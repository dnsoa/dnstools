package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/dnsoa/dnstools/pkg/commands"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := commands.Root.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
