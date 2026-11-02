//go:build integration

package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Реальный PostgreSQL, отдельная временная схема. Рабочие таблицы не очищаются.
func TestPostgresPersistenceAndConcurrency(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" { t.Skip("set TEST_DATABASE_URL for an isolated test PostgreSQL database") }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", dsn)
	if err != nil { t.Fatal(err) }
	defer admin.Close()
	schema := fmt.Sprintf("ledger_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil { t.Fatal(err) }
	defer admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" { t.Fatal("TEST_DATABASE_URL must be a postgres:// URL") }
	query := u.Query(); query.Set("search_path", schema); u.RawQuery = query.Encode()
	open := func() *sql.DB {
		database, err := sql.Open("pgx", u.String())
		if err != nil { t.Fatal(err) }
		database.SetMaxOpenConns(10)
		return database
	}
	database := open()
	t.Cleanup(func() { database.Close() })
	for _, name := range []string{"00001_create_budgets.sql", "00002_create_expenses.sql"} {
		body, err := os.ReadFile("migrations/"+name)
		if err != nil { t.Fatal(err) }
		up := strings.Split(string(body), "-- +goose Down")[0]
		if _, err := database.ExecContext(ctx, up); err != nil { t.Fatal(err) }
	}
	book := NewLedger(database, nil)
	if err := book.SetBudget(Budget{Category: "еда", Limit: 10}); err != nil { t.Fatal(err) }
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- book.AddTransaction(Transaction{Category: "еда", Amount: 1, Date: "2025-09-10"})
		}()
	}
	wg.Wait(); close(results)
	accepted := 0
	for err := range results {
		if err == nil { accepted++ } else if err != ErrBudgetExceeded { t.Fatal(err) }
	}
	if accepted != 10 { t.Fatalf("accepted %d expenses, want 10", accepted) }
	if err := book.Close(); err != nil { t.Fatal(err) }
	// Новое соединение имитирует перезапуск Ledger.
	reopened := NewLedger(open(), nil)
	defer reopened.Close()
	transactions, err := reopened.ListTransactions()
	if err != nil || len(transactions) != 10 { t.Fatalf("persistence failed: %d records, %v", len(transactions), err) }
	budgets, err := reopened.ListBudgets()
	if err != nil || len(budgets) != 1 || budgets[0].Limit != 10 { t.Fatalf("budgets not persisted: %+v, %v", budgets, err) }
	report, err := reopened.GetReportSummary(ctx, "2025-09-01", "2025-09-30")
	if err != nil || report.Total != 10 { t.Fatalf("report: %+v, %v", report, err) }
}
