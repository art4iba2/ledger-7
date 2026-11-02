package ledger

import (
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "sort"
)

// MemoryLedger — изолированная реализация для unit-тестов и старого демо.
// В рабочем Gateway используется Ledger с PostgreSQL.
type MemoryLedger struct {
	transactions []Transaction
	budgets      map[string]Budget
}

func NewMemoryLedger() *MemoryLedger {
	return &MemoryLedger{budgets: make(map[string]Budget)}
}

// Reset очищает состояние конкретного экземпляра, сохраняя изоляцию тестов.
// Вызывать только когда работа с этим экземпляром завершена.
func (l *MemoryLedger) Reset() {
	l.transactions = nil
	l.budgets = make(map[string]Budget)
}

// SetBudget добавляет бюджет или заменяет существующий бюджет категории.
// Уменьшение лимита не удаляет уже записанные транзакции.
func (l *MemoryLedger) SetBudget(b Budget) error {
	if err := b.Validate(); err != nil {
		return fmt.Errorf("set budget: %w", err)
	}
	if l.budgets == nil {
		l.budgets = make(map[string]Budget)
	}
	l.budgets[b.Category] = b
	return nil
}

func (l *MemoryLedger) AddTransaction(tx Transaction) error {
	if err := tx.Validate(); err != nil {
		return fmt.Errorf("add transaction: %w", err)
	}
	if b, ok := l.budgets[tx.Category]; ok {
		total := tx.Amount
		for _, existing := range l.transactions {
			if existing.Category == tx.Category {
				total += existing.Amount
			}
		}
		if total > b.Limit {
			return fmt.Errorf("%w: category %q, total %.2f, limit %.2f", ErrBudgetExceeded, tx.Category, total, b.Limit)
		}
	}
	l.transactions = append(l.transactions, tx)
	return nil
}

// Transactions возвращает копию, чтобы вызывающий код не менял учёт расходов.
func (l *MemoryLedger) Transactions() []Transaction {
	return append([]Transaction(nil), l.transactions...)
}

func (l *MemoryLedger) ListTransactions() []Transaction {
	return l.Transactions()
}

// ListBudgets возвращает копии бюджетов в стабильном порядке категорий.
func (l *MemoryLedger) ListBudgets() []Budget {
	result := make([]Budget, 0, len(l.budgets))
	for _, b := range l.budgets {
		result = append(result, b)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Category < result[j].Category })
	return result
}

// LoadBudgets принимает один JSON-массив. При ошибке хранилище не меняется.
func (l *MemoryLedger) LoadBudgets(r io.Reader) error {
	decoder := json.NewDecoder(r)
	var budgets []Budget
	if err := decoder.Decode(&budgets); err != nil {
		return fmt.Errorf("read or parse budgets JSON: %w", err)
	}
	if budgets == nil {
		return errors.New("budgets JSON must be an array, not null")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return fmt.Errorf("read trailing budgets JSON: %w", err)
		}
		return errors.New("budgets JSON must contain exactly one array")
	}
	for i, b := range budgets {
		if err := b.Validate(); err != nil {
			return fmt.Errorf("budget at index %d: %w", i, err)
		}
	}
	for _, b := range budgets {
		if err := l.SetBudget(b); err != nil {
			return err
		}
	}
	return nil
}
