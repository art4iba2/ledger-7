package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"ledger"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func call(t *testing.T, h http.Handler, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	if w.Code != status {
		t.Fatalf("%s %s: status %d, want %d; body %s", method, path, w.Code, status, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected Content-Type: %s", w.Header().Get("Content-Type"))
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("invalid JSON response: %s", w.Body.String())
	}
	if status >= 400 {
		var response map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response) != 1 || response["error"] == "" {
			t.Fatalf("unexpected error format: %v", response)
		}
	}
	return w
}

func newTestHandler(t *testing.T) (http.Handler, *ledger.MemoryLedger) {
 t.Helper()
 book := ledger.NewMemoryLedger()
 book.Reset()
 t.Cleanup(book.Reset)
 return NewHandler(memoryStore{book}), book
}

func TestBudgets(t *testing.T) {
 for _, name := range []string{"ok", "bad_json", "zero_limit", "negative_limit"} {
  t.Run(name, func(t *testing.T) {
   h, book := newTestHandler(t)
   switch name {
   case "ok":
    w := call(t, h, "POST", "/api/budgets", `{"category":"еда","limit":5000}`, 201)
    var response BudgetResponse
    if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil { t.Fatal(err) }
    if response.Category != "еда" || response.Limit != 5000 { t.Fatalf("wrong DTO: %+v", response) }
    w = call(t, h, "GET", "/api/budgets", "", 200)
    var list []BudgetResponse
    if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil { t.Fatal(err) }
    if len(list) != 1 || list[0] != response { t.Fatalf("budget missing from list: %+v", list) }
    call(t, h, "POST", "/api/budgets", `{"category":"еда","limit":6000}`, 201)
    if list := book.ListBudgets(); len(list) != 1 || list[0].Limit != 6000 { t.Fatal("budget was not updated") }
   default:
    body := "{"
    if name == "zero_limit" { body = `{"category":"еда","limit":0}` }
    if name == "negative_limit" { body = `{"category":"еда","limit":-1}` }
    call(t, h, "POST", "/api/budgets", body, 400)
    if len(book.ListBudgets()) != 0 { t.Fatal("invalid budget was saved") }
   }
  })
 }
}

func TestTransactions(t *testing.T) {
 for _, name := range []string{"ok", "exceeded", "bad_json"} {
  t.Run(name, func(t *testing.T) {
   h, book := newTestHandler(t)
   call(t, h, "POST", "/api/budgets", `{"category":"еда","limit":5000}`, 201)
   body := `{"amount":4500,"category":"еда","description":"ланч","date":"2025-09-10"}`
   w := call(t, h, "POST", "/api/transactions", body, 201)
   var created TransactionResponse
   if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil { t.Fatal(err) }
   want := TransactionResponse{Amount: 4500, Category: "еда", Description: "ланч", Date: "2025-09-10"}
   if created != want { t.Fatalf("created = %+v, want %+v", created, want) }
   switch name {
   case "ok":
    w = call(t, h, "GET", "/api/transactions", "", 200)
    var list []TransactionResponse
    if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil { t.Fatal(err) }
    if len(list) != 1 || list[0] != created { t.Fatalf("wrong list: %+v", list) }
    call(t, h, "POST", "/api/transactions", `{"amount":500,"category":"еда"}`, 201)
    call(t, h, "POST", "/api/transactions", `{"amount":9999,"category":"книги"}`, 201)
   case "exceeded":
    before := len(book.ListTransactions())
    w = call(t, h, "POST", "/api/transactions", `{"amount":501,"category":"еда"}`, 409)
    if strings.TrimSpace(w.Body.String()) != `{"error":"budget exceeded"}` { t.Fatal(w.Body.String()) }
    if len(book.ListTransactions()) != before { t.Fatal("rejected transaction was saved") }
   case "bad_json":
    call(t, h, "POST", "/api/transactions", "{", 400)
    if len(book.ListTransactions()) != 1 { t.Fatal("bad JSON changed storage") }
   }
  })
 }
}

func TestEmptyLists(t *testing.T) {
 for _, path := range []string{"/api/transactions", "/api/budgets"} {
  t.Run(path, func(t *testing.T) {
   h, _ := newTestHandler(t)
   w := call(t, h, "GET", path, "", 200)
   if strings.TrimSpace(w.Body.String()) != "[]" { t.Fatal("expected empty JSON array") }
  })
 }
}

func TestBadRequestsDoNotChangeStorage(t *testing.T) {
	h, book := newTestHandler(t)
	for _, path := range []string{"/api/transactions", "/api/budgets"} {
		for _, body := range []string{"", "{", "null", "[]", `{}`, `{} {}`, `{} garbage`, `{"unexpected":1}`} {
			call(t, h, "POST", path, body, 400)
		}
	}
	for _, body := range []string{`{"amount":0,"category":"еда"}`, `{"amount":-1,"category":"еда"}`, `{"amount":1,"category":" "}`, `{"amount":"1","category":"еда"}`} {
		call(t, h, "POST", "/api/transactions", body, 400)
	}
	for _, body := range []string{`{"limit":0,"category":"еда"}`, `{"limit":-1,"category":"еда"}`, `{"limit":1,"category":" "}`} {
		call(t, h, "POST", "/api/budgets", body, 400)
	}
	oversized := `{"amount":1,"category":"еда","description":"` + strings.Repeat("x", 1<<20) + `"}`
	call(t, h, "POST", "/api/transactions", oversized, 400)
	if len(book.ListTransactions()) != 0 || len(book.ListBudgets()) != 0 { t.Fatal("bad request changed storage") }
}

type memoryStore struct { *ledger.MemoryLedger }
func (s memoryStore) ListTransactions() ([]ledger.Transaction, error) { return s.MemoryLedger.ListTransactions(), nil }
func (s memoryStore) ListBudgets() ([]ledger.Budget, error) { return s.MemoryLedger.ListBudgets(), nil }

type failingStore struct { memoryStore }

func (f failingStore) AddTransaction(ledger.Transaction) error { return errors.New("private database failure") }
func (f failingStore) SetBudget(ledger.Budget) error { return errors.New("private database failure") }

func TestInternalErrors(t *testing.T) {
	book := ledger.NewMemoryLedger()
 t.Cleanup(book.Reset)
 h := NewHandler(failingStore{memoryStore{book}})
	for path, body := range map[string]string{
		"/api/transactions": `{"amount":1,"category":"еда"}`,
		"/api/budgets": `{"limit":1,"category":"еда"}`,
	} {
		w := call(t, h, "POST", path, body, 500)
		if strings.Contains(w.Body.String(), "private") { t.Fatal("internal error details leaked") }
	}
}

func TestRoutesAndLogging(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	h, _ := newTestHandler(t)
	call(t, h, "GET", "/missing", "", 404)
	w := call(t, h, "DELETE", "/api/budgets", "", 405)
	if w.Header().Get("Allow") != "GET, POST" { t.Fatal("missing Allow header") }
	for _, expected := range []string{"method=GET", "path=/missing", "method=DELETE", "path=/api/budgets", "duration="} {
		if !strings.Contains(output.String(), expected) { t.Fatalf("missing log field %q", expected) }
	}
}

func TestConcurrentRequestsRespectBudget(t *testing.T) {
	h, book := newTestHandler(t)
	if err := book.SetBudget(ledger.Budget{Category: "еда", Limit: 10}); err != nil { t.Fatal(err) }
	var wg sync.WaitGroup
	codes := make(chan int, 30)
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("POST", "/api/transactions", strings.NewReader(`{"amount":1,"category":"еда"}`)))
			codes <- w.Code
		}()
	}
	wg.Wait()
	close(codes)
	created := 0
	for code := range codes {
		if code == 201 { created++ } else if code != 409 { t.Fatalf("unexpected status %d", code) }
	}
	if created != 10 || len(book.ListTransactions()) != 10 { t.Fatal("concurrent requests exceeded budget") }
}

func ExampleNewHandler() {
	h := NewHandler(memoryStore{ledger.NewMemoryLedger()})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/transactions", nil))
	body, _ := io.ReadAll(w.Result().Body)
	fmt.Printf("%d %s", w.Code, body)
	// Output: 200 []
}
