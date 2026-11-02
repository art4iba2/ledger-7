// Проверка готовности зависимостей Ledger перед запуском Gateway.
package main

import (
	"context"
	"ledger"
	"log"
	"time"
)

func main() {
	if err := check(); err != nil { log.Fatal(err) }
}

func check() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	book, err := ledger.Open(ctx)
	if err != nil { return err }
	defer book.Close()
	if _, err := book.ListBudgets(); err != nil { return err }
	if _, err := book.ListTransactions(); err != nil { return err }
	log.Print("Ledger ready: PostgreSQL, Redis and schema available")
	return nil
}
