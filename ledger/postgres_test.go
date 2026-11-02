package ledger

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func testSQLLedger(t *testing.T) (*Ledger, sqlmock.Sqlmock) {
	t.Helper()
	database, mock, err := sqlmock.New()
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil { t.Error(err) }
		database.Close()
	})
	return NewLedger(database, nil), mock
}

func expectCategoryLock(mock sqlmock.Sqlmock) {
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("еда").WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestPostgresAddTransaction(t *testing.T) {
	for _, name := range []string{"ok", "no_budget", "exceeded", "database_error"} {
		t.Run(name, func(t *testing.T) {
			book, mock := testSQLLedger(t)
			mock.ExpectBegin()
			expectCategoryLock(mock)
			query := mock.ExpectQuery("SELECT limit_amount FROM budgets").WithArgs("еда")
			if name == "no_budget" {
				query.WillReturnError(sql.ErrNoRows)
			} else if name == "database_error" {
				query.WillReturnError(errors.New("database unavailable"))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"limit_amount"}).AddRow(5000))
				mock.ExpectQuery("SELECT COALESCE").WithArgs("еда", 450.0, 5000.0).
					WillReturnRows(sqlmock.NewRows([]string{"exceeded"}).AddRow(name == "exceeded"))
			}
			if name == "exceeded" || name == "database_error" {
				mock.ExpectRollback()
			} else {
				mock.ExpectQuery("INSERT INTO expenses").WithArgs(450.0, "еда", "ланч", "2025-09-10").
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
				mock.ExpectCommit()
			}
			err := book.AddTransaction(Transaction{Amount: 450, Category: "еда", Description: "ланч", Date: "2025-09-10"})
			if name == "exceeded" {
				if !errors.Is(err, ErrBudgetExceeded) { t.Fatalf("expected budget error: %v", err) }
			} else if name == "database_error" {
				if err == nil { t.Fatal("database error lost") }
			} else if err != nil { t.Fatal(err) }
		})
	}
}

func TestPostgresSetBudget(t *testing.T) {
	book, mock := testSQLLedger(t)
	if err := book.SetBudget(Budget{Category: "еда", Limit: 0}); err == nil { t.Fatal("invalid budget accepted") }
	mock.ExpectBegin()
	expectCategoryLock(mock)
	mock.ExpectExec("INSERT INTO budgets").WithArgs("еда", 5000.0).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if err := book.SetBudget(Budget{Category: "еда", Limit: 5000}); err != nil { t.Fatal(err) }
}

func TestPostgresLists(t *testing.T) {
	book, mock := testSQLLedger(t)
	mock.ExpectQuery("SELECT category, limit_amount FROM budgets ORDER BY category").
		WillReturnRows(sqlmock.NewRows([]string{"category", "limit_amount"}).AddRow("еда", 5000))
	budgets, err := book.ListBudgets()
	if err != nil || len(budgets) != 1 || budgets[0].Limit != 5000 { t.Fatalf("budgets = %+v, %v", budgets, err) }
	date := time.Date(2025, 9, 10, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT id, amount, category, COALESCE").
		WillReturnRows(sqlmock.NewRows([]string{"id", "amount", "category", "description", "date"}).AddRow(42, 450, "еда", "ланч", date))
	transactions, err := book.ListTransactions()
	if err != nil || len(transactions) != 1 || transactions[0].ID != 42 || transactions[0].Date != "2025-09-10" {
		t.Fatalf("transactions = %+v, %v", transactions, err)
	}
	mock.ExpectQuery("SELECT category").WillReturnError(errors.New("offline"))
	if _, err := book.ListBudgets(); err == nil { t.Fatal("read error lost") }
	mock.ExpectQuery("SELECT id").WillReturnError(errors.New("offline"))
	if _, err := book.ListTransactions(); err == nil { t.Fatal("read error lost") }
}

func TestReportCache(t *testing.T) {
	book, mock := testSQLLedger(t)
	server := miniredis.RunT(t)
	book.cache = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { book.cache.Close() })
	key := "report:summary:2025-09-01:2025-09-30"
	expectQuery := func(amount float64) {
		mock.ExpectQuery("SELECT category, SUM").WithArgs("2025-09-01", "2025-09-30").
			WillReturnRows(sqlmock.NewRows([]string{"category", "sum"}).AddRow("еда", amount))
	}
	expectQuery(450)
	first, err := book.GetReportSummary(context.Background(), "2025-09-01", "2025-09-30")
	if err != nil || first.Total != 450 { t.Fatalf("cache miss: %+v, %v", first, err) }
	if !server.Exists(key) || server.TTL(key) != 30*time.Second { t.Fatal("cache TTL not set") }
	// Для cache hit нет ожидания SQL: повторный запрос в БД провалит тест.
	second, err := book.GetReportSummary(context.Background(), "2025-09-01", "2025-09-30")
	if err != nil || second.Total != first.Total { t.Fatalf("cache hit: %+v, %v", second, err) }
	server.FastForward(31*time.Second)
	expectQuery(500)
	third, err := book.GetReportSummary(context.Background(), "2025-09-01", "2025-09-30")
	if err != nil || third.Total != 500 { t.Fatalf("expired cache: %+v, %v", third, err) }
	server.Set(key, "corrupt JSON")
	expectQuery(600)
	if result, err := book.GetReportSummary(context.Background(), "2025-09-01", "2025-09-30"); err != nil || result.Total != 600 {
		t.Fatalf("corrupt cache did not fall back to DB: %+v, %v", result, err)
	}
}

func TestValidateReportDates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ from, to string; wantError bool }{
		{"2025-09-01", "2025-09-30", false},
		{"2025-09-10", "2025-09-10", false},
		{"", "2025-09-30", true},
		{"2025-09-01", "2025-02-30", true},
		{"2025-09-30", "2025-09-01", true},
	} {
		if err := ValidateReportDates(tc.from, tc.to); (err != nil) != tc.wantError { t.Fatalf("%+v: %v", tc, err) }
	}
}
