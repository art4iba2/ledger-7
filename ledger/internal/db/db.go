package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" { return value }
	return fallback
}

func DSN() string {
	if value := os.Getenv("DATABASE_URL"); value != "" { return value }
	u := &url.URL{
		Scheme: "postgres",
		Host: net.JoinHostPort(env("DB_HOST", "localhost"), env("DB_PORT", "5432")),
		User: url.UserPassword(env("DB_USER", "postgres"), env("DB_PASS", "postgres")),
		Path: "/" + env("DB_NAME", "cashapp"),
	}
	query := u.Query()
	query.Set("sslmode", env("DB_SSLMODE", "disable"))
	u.RawQuery = query.Encode()
	return u.String()
}

func Open(ctx context.Context) (*sql.DB, error) {
	database, err := sql.Open("pgx", DSN())
	if err != nil { return nil, fmt.Errorf("initialize PostgreSQL driver: %w", err) }
	database.SetMaxOpenConns(10)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(30 * time.Minute)
	if err := database.PingContext(ctx); err != nil {
		database.Close()
		// Не включаем DSN и пароль в текст ошибки.
		return nil, fmt.Errorf("connect PostgreSQL: check DATABASE_URL or DB_* settings and server availability: %w", err)
	}
	return database, nil
}
