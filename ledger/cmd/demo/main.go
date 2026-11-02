package main

import (
	"bufio"
	"fmt"
	"log"
	"os"

	"ledger"
)

func main() {
	// Одна функция работает с разными типами через интерфейс Validatable.
	for _, v := range []ledger.Validatable{
		ledger.Transaction{Category: "еда", Amount: 100, Date: "2026-11-02"},
		ledger.Budget{Category: "еда", Limit: 5000},
		ledger.Transaction{Category: "еда", Amount: 0},
		ledger.Budget{Category: "", Limit: 100},
	} {
		if err := ledger.CheckValid(v); err != nil {
			fmt.Printf("Проверка %T: %v\n", v, err)
		} else {
			fmt.Printf("Проверка %T: данные корректны\n", v)
		}
	}

	book := ledger.NewMemoryLedger()
	for _, b := range []ledger.Budget{
		{Category: "еда", Limit: 5000},
		{Category: "транспорт", Limit: 2000},
	} {
		if err := book.SetBudget(b); err != nil {
			log.Fatalf("Не удалось установить бюджет: %v", err)
		}
	}

	file, err := os.Open("budgets.json")
	if err != nil {
		log.Fatalf("Не удалось открыть budgets.json: %v", err)
	}
	defer file.Close()
	if err := book.LoadBudgets(bufio.NewReader(file)); err != nil {
		log.Fatalf("Не удалось загрузить бюджеты: %v", err)
	}

	for _, tx := range []ledger.Transaction{
		{Category: "еда", Amount: 3000}, // успех: 3000 из 5000
		{Category: "еда", Amount: 2000}, // успех: ровно 5000
		{Category: "еда", Amount: 100},  // отказ: 5100 > 5000
		{Category: "транспорт", Amount: 500},
		{Category: "книги", Amount: 700}, // категория без бюджета
	} {
		if err := book.AddTransaction(tx); err != nil {
			fmt.Printf("Отказ: %s, %.2f руб.: %v\n", tx.Category, tx.Amount, err)
			continue
		}
		fmt.Printf("Добавлено: %s, %.2f руб.\n", tx.Category, tx.Amount)
	}

	fmt.Printf("Сохранено транзакций: %d\n", len(book.Transactions()))
	for _, tx := range book.Transactions() {
		fmt.Printf("  %s: %.2f руб.\n", tx.Category, tx.Amount)
	}
}
