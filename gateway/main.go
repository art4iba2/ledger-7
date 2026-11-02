package main

import (
	"context"
	"errors"
	"gateway/internal/api"
	"ledger"
	"log"
	"net/http"
	"time"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(); err != nil { log.Fatal(err) }
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	book, err := ledger.Open(ctx)
	cancel()
	if err != nil { return err }
	defer book.Close()
	server := &http.Server{
		Addr:              ":8080",
		Handler:           api.NewHandler(book),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	stop, stopCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopCancel()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-stop.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil { log.Printf("shutdown: %v", err) }
	}()
	log.Printf("Gateway listening on %s", server.Addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) { <-shutdownDone; return nil }
	return err
}
