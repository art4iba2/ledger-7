package api

import (
	"ledger"
	"time"
)

type CreateTransactionRequest struct {
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"`
}

type TransactionResponse struct {
	Amount      float64 `json:"amount"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Date        string  `json:"date"`
}

type CreateBudgetRequest struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

type BudgetResponse struct {
	Category string  `json:"category"`
	Limit    float64 `json:"limit"`
}

func transactionFromRequest(r CreateTransactionRequest) ledger.Transaction {
	if r.Date == "" { r.Date = time.Now().UTC().Format("2006-01-02") }
	return ledger.Transaction{Amount: r.Amount, Category: r.Category, Description: r.Description, Date: r.Date}
}

func transactionToResponse(tx ledger.Transaction) TransactionResponse {
	return TransactionResponse{Amount: tx.Amount, Category: tx.Category, Description: tx.Description, Date: tx.Date}
}

func budgetFromRequest(r CreateBudgetRequest) ledger.Budget {
	return ledger.Budget{Category: r.Category, Limit: r.Limit}
}

func budgetToResponse(b ledger.Budget) BudgetResponse {
	return BudgetResponse{Category: b.Category, Limit: b.Limit}
}
