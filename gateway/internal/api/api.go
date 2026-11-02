package api

import (
	"encoding/json"
	"context"
	"errors"
	"io"
	"ledger"
	"log"
	"net/http"
	"sync"
	"time"
)

// Store позволяет тестировать HTTP отдельно от доменной реализации.
type Store interface {
	AddTransaction(ledger.Transaction) error
	ListTransactions() ([]ledger.Transaction, error)
	SetBudget(ledger.Budget) error
	ListBudgets() ([]ledger.Budget, error)
}

type API struct {
	store Store
	// Сохранена для совместимости с тестовыми хранилищами.
	// Рабочий Ledger также защищает бюджет транзакцией PostgreSQL.
	mu sync.Mutex
}

func NewHandler(store Store) http.Handler {
	a := &API{store: store}
	return LoggingMiddleware(http.HandlerFunc(a.serveHTTP))
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			log.Printf("method=%s path=%s duration=%s", r.Method, r.URL.Path, time.Since(start))
		}()
		next.ServeHTTP(w, r)
	})
}

func (a *API) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.URL.Path {
	case "/api/reports/summary":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		a.reportSummary(w, r)
	case "/api/transactions":
		switch r.Method {
		case http.MethodPost:
			a.createTransaction(w, r)
		case http.MethodGet:
			a.listTransactions(w, r)
		default:
			methodNotAllowed(w)
		}
	case "/api/budgets":
		switch r.Method {
		case http.MethodPost:
			a.createBudget(w, r)
		case http.MethodGet:
			a.listBudgets(w, r)
		default:
			methodNotAllowed(w)
		}
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// Ограничиваем размер тела и принимаем ровно один JSON-объект.
func decodeRequest(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("expected a single JSON object")
	}
	return nil
}

func (a *API) createTransaction(w http.ResponseWriter, r *http.Request) {
	var request CreateTransactionRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	tx := transactionFromRequest(request)
	if err := tx.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.mu.Lock()
	err := a.store.AddTransaction(tx)
	a.mu.Unlock()
	if err != nil {
		if errors.Is(err, ledger.ErrBudgetExceeded) {
			writeError(w, http.StatusConflict, "budget exceeded")
		} else {
			log.Printf("add transaction: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, transactionToResponse(tx))
}

func (a *API) listTransactions(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	transactions, err := a.store.ListTransactions()
	a.mu.Unlock()
	if err != nil {
		log.Printf("list transactions: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]TransactionResponse, 0, len(transactions))
	for _, tx := range transactions {
		response = append(response, transactionToResponse(tx))
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *API) createBudget(w http.ResponseWriter, r *http.Request) {
	var request CreateBudgetRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	b := budgetFromRequest(request)
	if err := b.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.mu.Lock()
	err := a.store.SetBudget(b)
	a.mu.Unlock()
	if err != nil {
		log.Printf("set budget: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, budgetToResponse(b))
}

func (a *API) listBudgets(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	budgets, err := a.store.ListBudgets()
	a.mu.Unlock()
	if err != nil {
		log.Printf("list budgets: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	response := make([]BudgetResponse, 0, len(budgets))
	for _, b := range budgets {
		response = append(response, budgetToResponse(b))
	}
	writeJSON(w, http.StatusOK, response)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{Error: message})
}

func (a *API) reportSummary(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if err := ledger.ValidateReportDates(from, to); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reporter, ok := a.store.(interface {
		GetReportSummary(context.Context, string, string) (ledger.ReportSummary, error)
	})
	if !ok { writeError(w, http.StatusInternalServerError, "internal server error"); return }
	summary, err := reporter.GetReportSummary(r.Context(), from, to)
	if err != nil {
		log.Printf("report summary: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	// Кодируем до отправки статуса, чтобы ошибка сериализации тоже стала JSON 500.
	body, err := json.Marshal(value)
	if err != nil {
		log.Printf("encode response: %v", err)
		status = http.StatusInternalServerError
		body = []byte(`{"error":"internal server error"}`)
	}
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		log.Printf("write response: %v", err)
	}
}
