package cache

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

func Open(ctx context.Context) (*redis.Client, error) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" { addr = "localhost:6379" }
	index := 0
	if value := os.Getenv("REDIS_DB"); value != "" {
		var err error
		index, err = strconv.Atoi(value)
		if err != nil || index < 0 { return nil, fmt.Errorf("REDIS_DB must be a non-negative integer") }
	}
	client := redis.NewClient(&redis.Options{
		Addr: addr, DB: index, Password: os.Getenv("REDIS_PASSWORD"),
		DialTimeout: time.Second, ReadTimeout: 500*time.Millisecond,
		WriteTimeout: 500*time.Millisecond, MaxRetries: 1, ContextTimeoutEnabled: true,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("connect Redis: check REDIS_ADDR, REDIS_DB and REDIS_PASSWORD: %w", err)
	}
	return client, nil
}
