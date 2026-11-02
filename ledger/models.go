package ledger

import (
	"errors"
	"math"
	"strings"
	"time"
)

// Validatable задаёт общий контракт проверки данных.
type Validatable interface {
	Validate() error
}

func CheckValid(v Validatable) error {
	return v.Validate()
}

// В этом примере каждая транзакция — расход с положительной суммой.
type Transaction struct {
 ID int64 `json:"id,omitempty"`
	Description string `json:"description"`
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
	Date     string  `json:"date,omitempty"`
}

// Budget задаёт постоянный лимит категории, без разделения по периодам.
type Budget struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

var (
	_ Validatable = Transaction{}
	_ Validatable = Budget{}
)

// Validate только возвращает ошибку; решение о логировании принимает вызывающий код.
func (tx Transaction) Validate() error {
	if tx.Amount <= 0 || math.IsNaN(tx.Amount) || math.IsInf(tx.Amount, 0) {
		return errors.New("transaction amount must be a finite positive number")
	}
	if strings.TrimSpace(tx.Category) == "" {
		return errors.New("transaction category must not be empty")
	}
	if !validMoney(tx.Amount) { return errors.New("transaction amount must fit NUMERIC(14,2) and have at most two decimal places") }
	if tx.Date != "" {
		if _, err := time.Parse("2006-01-02", tx.Date); err != nil {
			return errors.New("transaction date must be a valid YYYY-MM-DD date")
		}
	}
	return nil
}

func (b Budget) Validate() error {
	if b.Limit <= 0 || math.IsNaN(b.Limit) || math.IsInf(b.Limit, 0) {
		return errors.New("budget limit must be a finite positive number")
	}
	if strings.TrimSpace(b.Category) == "" {
		return errors.New("budget category must not be empty")
	}
	if !validMoney(b.Limit) { return errors.New("budget limit must fit NUMERIC(14,2) and have at most two decimal places") }
	return nil
}

func validMoney(value float64) bool {
	return value <= 999999999999.99 && math.Abs(value*100-math.Round(value*100)) < 0.001
}
