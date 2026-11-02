package ledger

import (
	"math"
	"strings"
	"testing"
)

func TestCheckValid(t *testing.T) {
 t.Parallel()
	cases := []struct {
		name    string
		value   Validatable
		wantErr string
	}{
		{"valid transaction", Transaction{Category: "еда", Amount: 100, Date: "2026-11-02"}, ""},
		{"zero amount", Transaction{Category: "еда", Amount: 0}, "amount"},
		{"negative amount", Transaction{Category: "еда", Amount: -1}, "amount"},
		{"nan amount", Transaction{Category: "еда", Amount: math.NaN()}, "amount"},
		{"infinite amount", Transaction{Category: "еда", Amount: math.Inf(1)}, "amount"},
		{"empty transaction category", Transaction{Amount: 100}, "category"},
		{"blank transaction category", Transaction{Category: " \t", Amount: 100}, "category"},
		{"valid budget", Budget{Category: "еда", Limit: 5000}, ""},
		{"zero limit", Budget{Category: "еда", Limit: 0}, "limit"},
		{"negative limit", Budget{Category: "еда", Limit: -1}, "limit"},
		{"nan limit", Budget{Category: "еда", Limit: math.NaN()}, "limit"},
		{"infinite limit", Budget{Category: "еда", Limit: math.Inf(1)}, "limit"},
		{"empty budget category", Budget{Limit: 100}, "category"},
		{"blank budget category", Budget{Category: " \t", Limit: 100}, "category"},
	}
	for _, tc := range cases {
 tc := tc
		t.Run(tc.name, func(t *testing.T) {
 t.Parallel()
			err := CheckValid(tc.value)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSetBudgetRejectsInvalidData(t *testing.T) {
	var l MemoryLedger
 t.Cleanup(l.Reset)
	if err := l.SetBudget(Budget{Category: "еда", Limit: 100}); err != nil {
		t.Fatal(err)
	}
	for _, b := range []Budget{
		{Category: "еда", Limit: 0},
		{Category: "еда", Limit: -1},
		{Category: "еда", Limit: math.NaN()},
		{Category: "еда", Limit: math.Inf(1)},
		{Category: "", Limit: 100},
		{Category: " \t", Limit: 100},
	} {
		if err := l.SetBudget(b); err == nil {
			t.Fatalf("accepted invalid budget: %+v", b)
		}
		if len(l.budgets) != 1 || l.budgets["еда"].Limit != 100 {
			t.Fatal("invalid budget changed storage")
		}
	}
}

func TestAddTransactionRejectsEmptyCategory(t *testing.T) {
	var l MemoryLedger
 t.Cleanup(l.Reset)
	for _, category := range []string{"", " \t"} {
		if err := l.AddTransaction(Transaction{Category: category, Amount: 100}); err == nil {
			t.Fatal("accepted empty category")
		}
	}
	if len(l.Transactions()) != 0 {
		t.Fatal("invalid transaction was saved")
	}
}

func TestLoadBudgetsRejectsZeroLimitAtomically(t *testing.T) {
	var l MemoryLedger
 t.Cleanup(l.Reset)
	if err := l.SetBudget(Budget{Category: "еда", Limit: 100}); err != nil {
		t.Fatal(err)
	}
	err := l.LoadBudgets(strings.NewReader(`[{"category":"еда","limit":200},{"category":"книги","limit":0}]`))
	if err == nil {
		t.Fatal("accepted zero limit")
	}
	if len(l.budgets) != 1 || l.budgets["еда"].Limit != 100 {
		t.Fatal("failed load changed storage")
	}
}
