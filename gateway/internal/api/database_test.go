package api

import (
	"context"
	"errors"
	"ledger"
	"testing"
)

type unavailableLists struct{ memoryStore }
func (s unavailableLists) ListTransactions() ([]ledger.Transaction, error) { return nil, errors.New("SQL offline") }
func (s unavailableLists) ListBudgets() ([]ledger.Budget, error) { return nil, errors.New("SQL offline") }

func TestDatabaseReadErrors(t *testing.T) {
	book := ledger.NewMemoryLedger()
	t.Cleanup(book.Reset)
	h := NewHandler(unavailableLists{memoryStore{book}})
	for _, path := range []string{"/api/transactions", "/api/budgets"} {
		t.Run(path, func(t *testing.T) { call(t, h, "GET", path, "", 500) })
	}
}

type reportingStore struct {
	memoryStore
	err error
}

func (s reportingStore) GetReportSummary(ctx context.Context, from, to string) (ledger.ReportSummary, error) {
	return ledger.ReportSummary{From: from, To: to, Total: 450, Categories: []ledger.CategorySummary{{Category: "еда", Amount: 450}}}, s.err
}

func TestReportHTTP(t *testing.T) {
	for _, tc := range []struct { name, query string; err error; status int }{
		{"ok", "?from=2025-09-01&to=2025-09-30", nil, 200},
		{"missing_dates", "", nil, 400},
		{"bad_date", "?from=2025-02-30&to=2025-09-30", nil, 400},
		{"reversed", "?from=2025-09-30&to=2025-09-01", nil, 400},
		{"database_error", "?from=2025-09-01&to=2025-09-30", errors.New("SQL offline"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			book := ledger.NewMemoryLedger()
			t.Cleanup(book.Reset)
			h := NewHandler(reportingStore{memoryStore{book}, tc.err})
			call(t, h, "GET", "/api/reports/summary"+tc.query, "", tc.status)
		})
	}
}
