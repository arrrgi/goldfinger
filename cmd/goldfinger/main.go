// Command goldfinger answers Tailscale's OIDC WebFinger lookup.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/arrrgi/goldfinger/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
