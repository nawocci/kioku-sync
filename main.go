// Command kioku-sync serves the Kioku live-sync backend.
//
// Usage:
//
//	kioku-sync [flags]        run the server
//	kioku-sync key [flags]    mint an API key and print it
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nawocci/kioku-sync/internal/keys"
	"github.com/nawocci/kioku-sync/internal/server"
	"github.com/nawocci/kioku-sync/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags)

	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "key":
			os.Exit(runMintKey(args[1:]))
		case "serve":
			args = args[1:]
		}
	}
	os.Exit(runServe(args))
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", envOr("KIOKU_SYNC_ADDR", ":8756"), "listen address")
	dataDir := fs.String("data", envOr("KIOKU_SYNC_DATA", "data"), "data directory")
	fs.Parse(args)

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("creating data directory: %v", err)
	}
	st, err := store.Open(*dataDir + "/kioku-sync.db")
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer st.Close()
	static, err := dashboard()
	if err != nil {
		log.Fatalf("embedding dashboard: %v", err)
	}
	srv := server.New(st, server.Options{Static: static})
	srv.StartMaintenance(context.Background())

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", *addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serving: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	return 0
}

func runMintKey(args []string) int {
	fs := flag.NewFlagSet("key", flag.ExitOnError)
	dataDir := fs.String("data", envOr("KIOKU_SYNC_DATA", "data"), "data directory")
	label := fs.String("label", "", "library label; defaults to Key-N")
	fs.Parse(args)
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("creating data directory: %v", err)
	}
	st, err := store.Open(*dataDir + "/kioku-sync.db")
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer st.Close()

	key, err := keys.CreateKey(st.DB(), *label)
	if errors.Is(err, keys.ErrDuplicateLabel) {
		log.Fatalf("a library named %q already exists", *label)
	}
	if err != nil {
		log.Fatalf("minting key: %v", err)
	}
	fmt.Println(key)
	return 0
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
