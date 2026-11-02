package ledger

import "testing"

func TestTransactionValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tx Transaction
		wantError bool
	}{
		{"ok", Transaction{Amount: 450, Category: "еда", Date: "2025-09-10"}, false},
		{"zero", Transaction{Amount: 0, Category: "еда"}, true},
		{"negative", Transaction{Amount: -1, Category: "еда"}, true},
		{"empty_category", Transaction{Amount: 1}, true},
		{"blank_category", Transaction{Amount: 1, Category: " \t"}, true},
		{"invalid_date", Transaction{Amount: 1, Category: "еда", Date: "2025-02-30"}, true},
		{"subcent", Transaction{Amount: 0.001, Category: "еда"}, true},
		{"too_large", Transaction{Amount: 1000000000000, Category: "еда"}, true},
	}
	for _, tc := range cases {
		tc := tc // Совместимость с семантикой циклов Go 1.20.
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.tx.Validate(); (err != nil) != tc.wantError {
				t.Fatalf("Validate() = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestBudgetValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		budget Budget
		wantError bool
	}{
		{"ok", Budget{Category: "еда", Limit: 5000}, false},
		{"zero", Budget{Category: "еда", Limit: 0}, true},
		{"negative", Budget{Category: "еда", Limit: -1}, true},
		{"empty_category", Budget{Limit: 1}, true},
		{"blank_category", Budget{Category: " \t", Limit: 1}, true},
		{"subcent", Budget{Category: "еда", Limit: 0.001}, true},
		{"too_large", Budget{Category: "еда", Limit: 1000000000000}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.budget.Validate(); (err != nil) != tc.wantError {
				t.Fatalf("Validate() = %v, wantError %v", err, tc.wantError)
			}
		})
	}
}

func TestReset(t *testing.T) {
	l := newTestLedger(t)
	if err := l.SetBudget(Budget{Category: "еда", Limit: 5000}); err != nil { t.Fatal(err) }
	if err := l.AddTransaction(Transaction{Category: "еда", Amount: 100}); err != nil { t.Fatal(err) }
	l.Reset()
	l.Reset() // Повторная очистка также безопасна.
	if len(l.ListTransactions()) != 0 || len(l.ListBudgets()) != 0 {
		t.Fatal("Reset did not clear transactions and budgets")
	}
	if err := l.AddTransaction(Transaction{Category: "еда", Amount: 6000}); err != nil {
		t.Fatalf("old budget survived Reset: %v", err)
	}
	if err := l.SetBudget(Budget{Category: "книги", Limit: 100}); err != nil { t.Fatal(err) }
}

func TestListBudgetsReturnsCopies(t *testing.T) {
	l := newTestLedger(t)
	for _, b := range []Budget{{Category: "транспорт", Limit: 100}, {Category: "еда", Limit: 5000}} {
		if err := l.SetBudget(b); err != nil { t.Fatal(err) }
	}
	budgets := l.ListBudgets()
	if len(budgets) != 2 || budgets[0].Category != "еда" { t.Fatal("unexpected budget list") }
	budgets[0].Limit = 1
	if l.ListBudgets()[0].Limit != 5000 { t.Fatal("list changed stored budget") }
}
