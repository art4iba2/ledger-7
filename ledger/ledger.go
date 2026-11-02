package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"ledger/internal/cache"
	"ledger/internal/db"
	"github.com/redis/go-redis/v9"
)

var ErrBudgetExceeded = errors.New("budget exceeded")

type Ledger struct {
	db *sql.DB
	cache *redis.Client
}

func NewLedger(database *sql.DB, client *redis.Client) *Ledger {
	return &Ledger{db: database, cache: client}
}

// Open подключает PostgreSQL и Redis. Миграции применяются Goose до запуска.
func Open(ctx context.Context) (*Ledger, error) {
	database, err := db.Open(ctx)
	if err != nil { return nil, err }
	log.Print("Ledger: PostgreSQL connected")
	client, err := cache.Open(ctx)
	if err != nil { database.Close(); return nil, err }
	log.Print("Ledger: Redis connected")
	return NewLedger(database, client), nil
}

func (l *Ledger) Close() error {
	var cacheErr error
	if l.cache != nil { cacheErr = l.cache.Close() }
	dbErr := l.db.Close()
	if dbErr != nil { return dbErr }
	return cacheErr
}

func operationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5 * time.Second)
}

// Одинаковая блокировка категории используется при смене бюджета и расходе.
// Она работает даже до создания бюджета и между разными процессами Gateway.
func lockCategory(ctx context.Context, tx *sql.Tx, category string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, category)
	return err
}

func (l *Ledger) SetBudget(b Budget) error {
	if err := b.Validate(); err != nil { return fmt.Errorf("set budget: %w", err) }
	ctx, cancel := operationContext(); defer cancel()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil { return fmt.Errorf("begin budget update: %w", err) }
	defer tx.Rollback()
	if err := lockCategory(ctx, tx, b.Category); err != nil { return fmt.Errorf("lock category: %w", err) }
	_, err = tx.ExecContext(ctx, `INSERT INTO budgets(category, limit_amount) VALUES($1,$2)
 ON CONFLICT(category) DO UPDATE SET limit_amount=EXCLUDED.limit_amount`, b.Category, b.Limit)
	if err != nil { return fmt.Errorf("upsert budget: %w", err) }
	if err := tx.Commit(); err != nil { return fmt.Errorf("commit budget: %w", err) }
	return nil
}

func (l *Ledger) ListBudgets() ([]Budget, error) {
	ctx, cancel := operationContext(); defer cancel()
	rows, err := l.db.QueryContext(ctx, `SELECT category, limit_amount FROM budgets ORDER BY category`)
	if err != nil { return nil, fmt.Errorf("list budgets: %w", err) }
	defer rows.Close()
	result := make([]Budget, 0)
	for rows.Next() {
		var b Budget
		if err := rows.Scan(&b.Category, &b.Limit); err != nil { return nil, fmt.Errorf("scan budget: %w", err) }
		result = append(result, b)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("read budgets: %w", err) }
	return result, nil
}

func (l *Ledger) AddTransaction(value Transaction) error {
	if err := value.Validate(); err != nil { return fmt.Errorf("add transaction: %w", err) }
	// PostgreSQL DATE требует дату. Пустая дата по прежнему API разрешена:
	// для неё используем текущий день UTC. Непустая проверяется Validate.
	if value.Date == "" { value.Date = time.Now().UTC().Format("2006-01-02") }
	ctx, cancel := operationContext(); defer cancel()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil { return fmt.Errorf("begin expense: %w", err) }
	defer tx.Rollback()
	if err := lockCategory(ctx, tx, value.Category); err != nil { return fmt.Errorf("lock category: %w", err) }
	var limit float64
	err = tx.QueryRowContext(ctx, `SELECT limit_amount FROM budgets WHERE category=$1`, value.Category).Scan(&limit)
	if err != nil && !errors.Is(err, sql.ErrNoRows) { return fmt.Errorf("read budget: %w", err) }
	if err == nil {
		// Сравнение делается PostgreSQL в NUMERIC, как и запись суммы.
		var exceeded bool
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) + $2::numeric(14,2) > $3::numeric(14,2)
 FROM expenses WHERE category=$1`, value.Category, value.Amount, limit).Scan(&exceeded)
		if err != nil { return fmt.Errorf("sum expenses: %w", err) }
		if exceeded { return ErrBudgetExceeded }
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO expenses(amount, category, description, date)
 VALUES($1,$2,$3,$4) RETURNING id`, value.Amount, value.Category, value.Description, value.Date).Scan(&value.ID); err != nil {
		return fmt.Errorf("insert expense: %w", err)
	}
	if err := tx.Commit(); err != nil { return fmt.Errorf("commit expense: %w", err) }
	return nil
}

func (l *Ledger) ListTransactions() ([]Transaction, error) {
	ctx, cancel := operationContext(); defer cancel()
	rows, err := l.db.QueryContext(ctx, `SELECT id, amount, category, COALESCE(description,''), date
 FROM expenses ORDER BY date DESC, id DESC`)
	if err != nil { return nil, fmt.Errorf("list transactions: %w", err) }
	defer rows.Close()
	result := make([]Transaction, 0)
	for rows.Next() {
		var value Transaction
		var date time.Time
		if err := rows.Scan(&value.ID, &value.Amount, &value.Category, &value.Description, &date); err != nil {
			return nil, fmt.Errorf("scan expense: %w", err)
		}
		value.Date = date.Format("2006-01-02")
		result = append(result, value)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("read expenses: %w", err) }
	return result, nil
}

// LoadBudgets сначала проверяет весь JSON; записи применяются последовательно.
// При ошибке БД уже записанные бюджеты не откатываются.
func (l *Ledger) LoadBudgets(r io.Reader) error {
	decoder := json.NewDecoder(r)
	var budgets []Budget
	if err := decoder.Decode(&budgets); err != nil { return fmt.Errorf("read budgets JSON: %w", err) }
	if budgets == nil { return errors.New("budgets JSON must be an array") }
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil { return fmt.Errorf("read trailing budgets JSON: %w", err) }
		return errors.New("expected exactly one budgets array")
	}
	for i, b := range budgets { if err := b.Validate(); err != nil { return fmt.Errorf("budget %d: %w", i, err) } }
	for _, b := range budgets { if err := l.SetBudget(b); err != nil { return err } }
	return nil
}
