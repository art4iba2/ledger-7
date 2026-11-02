package ledger

import (
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

func newTestLedger(t *testing.T) *MemoryLedger {
 t.Helper()
 l := NewMemoryLedger()
 l.Reset()
 t.Cleanup(l.Reset)
 return l
}

func TestAddTransaction(t *testing.T) {
	t.Parallel()
 for _, tc := range []struct {
		name string
		category string
		amount float64
		wantError bool
	}{
		{"below limit", "еда", 1000, false},
		{"exact limit", "еда", 2000, false},
		{"exceeds limit", "еда", 2001, true},
		{"no budget", "книги", 10000, false},
	} {
		tc := tc
 t.Run(tc.name, func(t *testing.T) {
 t.Parallel()
			l := newTestLedger(t)
			if err := l.SetBudget(Budget{Category: "еда", Limit: 5000}); err != nil { t.Fatal(err) }
			if err := l.AddTransaction(Transaction{Category: "еда", Amount: 3000}); err != nil {
				t.Fatal(err)
			}
			err := l.AddTransaction(Transaction{Category: tc.category, Amount: tc.amount})
			if errors.Is(err, ErrBudgetExceeded) != tc.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.wantError && err != nil { t.Fatal(err) }
			wantCount := 2
			if tc.wantError { wantCount = 1 }
			if len(l.ListTransactions()) != wantCount { t.Fatal("rejected transaction was saved or accepted transaction was lost") }
		})
	}
}

func TestSetBudgetUpdateAndZeroLedger(t *testing.T) {
	var l MemoryLedger
 t.Cleanup(l.Reset)
	if err := l.SetBudget(Budget{Category: "еда", Limit: 100}); err != nil { t.Fatal(err) }
	if err := l.SetBudget(Budget{Category: "еда", Limit: 200}); err != nil { t.Fatal(err) }
	if err := l.AddTransaction(Transaction{Category: "еда", Amount: 200}); err != nil { t.Fatal(err) }
	if err := l.SetBudget(Budget{Category: "еда", Limit: 50}); err != nil { t.Fatal(err) }
	if err := l.AddTransaction(Transaction{Category: "еда", Amount: 1}); !errors.Is(err, ErrBudgetExceeded) { t.Fatalf("expected rejection: %v", err) }
	if len(l.Transactions()) != 1 { t.Fatal("existing transactions changed") }
}

func TestInvalidTransaction(t *testing.T) {
	for _, amount := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		var l MemoryLedger
 t.Cleanup(l.Reset)
		if err := l.AddTransaction(Transaction{Category: "еда", Amount: amount}); err == nil { t.Fatal("invalid amount accepted") }
		if len(l.Transactions()) != 0 { t.Fatal("invalid transaction saved") }
	}
}

func TestLoadBudgets(t *testing.T) {
	l := newTestLedger(t)
	if err := l.SetBudget(Budget{Category: "еда", Limit: 100}); err != nil { t.Fatal(err) }
	if err := l.LoadBudgets(strings.NewReader(`[{"category":"еда","limit":5000},{"category":"транспорт","limit":2000}]`)); err != nil { t.Fatal(err) }
	if l.budgets["еда"].Limit != 5000 || l.budgets["транспорт"].Limit != 2000 { t.Fatal("budgets were not loaded or updated") }
	if err := l.LoadBudgets(strings.NewReader(`[]`)); err != nil { t.Fatal(err) }
}

func TestLoadBudgetsErrorsAreAtomic(t *testing.T) {
	for _, input := range []string{
		``, `null`, `{}`, `[`, `[{"category":"еда","limit":"bad"}]`,
		`[{"category":"еда","limit":200}] {}`, `[] trailing`,
		`[{"category":"еда","limit":200},{"category":"транспорт","limit":-1}]`,
		`[{"category":"","limit":200}]`,
	} {
		t.Run(input, func(t *testing.T) {
			l := newTestLedger(t)
			if err := l.SetBudget(Budget{Category: "еда", Limit: 100}); err != nil { t.Fatal(err) }
			if err := l.LoadBudgets(strings.NewReader(input)); err == nil { t.Fatal("expected a JSON or validation error") }
			if len(l.budgets) != 1 || l.budgets["еда"].Limit != 100 { t.Fatal("budgets changed after failed load") }
		})
	}
}

type failingReader struct { err error }
func (r failingReader) Read(p []byte) (int, error) { return 0, r.err }

func TestLoadBudgetsReadError(t *testing.T) {
	want := errors.New("reader failed")
	var l MemoryLedger
 t.Cleanup(l.Reset)
	if err := l.LoadBudgets(failingReader{err: want}); !errors.Is(err, want) { t.Fatalf("reader error was lost: %v", err) }
	// Ошибка чтения после корректного массива также не должна менять бюджеты.
	r := io.MultiReader(strings.NewReader(`[{"category":"еда","limit":100}]`), failingReader{err: want})
	if err := l.LoadBudgets(r); !errors.Is(err, want) { t.Fatalf("trailing reader error was lost: %v", err) }
	if len(l.budgets) != 0 { t.Fatal("budgets changed after read error") }
}

func TestTransactionsReturnsCopy(t *testing.T) {
	var l MemoryLedger
 t.Cleanup(l.Reset)
	if err := l.AddTransaction(Transaction{Category: "еда", Amount: 100}); err != nil { t.Fatal(err) }
	copy := l.Transactions()
	copy[0].Amount = 0
	if l.Transactions()[0].Amount != 100 { t.Fatal("external mutation changed ledger") }
}
