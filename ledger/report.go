package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

type CategorySummary struct {
	Category string `json:"category"`
	Amount float64 `json:"amount"`
}

type ReportSummary struct {
	From string `json:"from"`
	To string `json:"to"`
	Total float64 `json:"total"`
	Categories []CategorySummary `json:"categories"`
}

func ValidateReportDates(from, to string) error {
	start, err := time.Parse("2006-01-02", from)
	if err != nil { return errors.New("from must be a valid YYYY-MM-DD date") }
	end, err := time.Parse("2006-01-02", to)
	if err != nil { return errors.New("to must be a valid YYYY-MM-DD date") }
	if start.After(end) { return errors.New("from must not be after to") }
	return nil
}

func (l *Ledger) GetReportSummary(ctx context.Context, from, to string) (ReportSummary, error) {
	if err := ValidateReportDates(from, to); err != nil { return ReportSummary{}, err }
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second); defer cancel()
	key := "report:summary:" + from + ":" + to
	if l.cache != nil {
		cacheCtx, cacheCancel := context.WithTimeout(ctx, 300*time.Millisecond)
		cached, err := l.cache.Get(cacheCtx, key).Bytes()
		cacheCancel()
		if err == nil {
			var result ReportSummary
			if json.Unmarshal(cached, &result) == nil && result.From == from && result.To == to && result.Categories != nil {
				log.Printf("report cache hit key=%s", key)
				return result, nil
			}
		} else if !errors.Is(err, redis.Nil) { log.Printf("report cache read failed: %v", err) }
	}
	log.Printf("report cache miss key=%s", key)
	rows, err := l.db.QueryContext(ctx, `SELECT category, SUM(amount) FROM expenses
 WHERE date >= $1::date AND date <= $2::date GROUP BY category ORDER BY category`, from, to)
	if err != nil { return ReportSummary{}, fmt.Errorf("report query: %w", err) }
	defer rows.Close()
	result := ReportSummary{From: from, To: to, Categories: make([]CategorySummary, 0)}
	for rows.Next() {
		var category CategorySummary
		if err := rows.Scan(&category.Category, &category.Amount); err != nil { return ReportSummary{}, fmt.Errorf("scan report: %w", err) }
		result.Total += category.Amount
		result.Categories = append(result.Categories, category)
	}
	if err := rows.Err(); err != nil { return ReportSummary{}, fmt.Errorf("read report: %w", err) }
	result.Total = math.Round(result.Total*100)/100
	if l.cache != nil {
		if body, err := json.Marshal(result); err == nil {
			cacheCtx, cacheCancel := context.WithTimeout(ctx, 300*time.Millisecond)
			if err := l.cache.Set(cacheCtx, key, body, 30*time.Second).Err(); err != nil { log.Printf("report cache set failed: %v", err) }
			cacheCancel()
		}
	}
	return result, nil
}
