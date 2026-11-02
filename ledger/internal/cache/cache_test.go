package cache

import (
	"context"
	"testing"
	"github.com/alicebob/miniredis/v2"
)

func TestOpen(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		server := miniredis.RunT(t)
		server.RequireAuth("secret")
		t.Setenv("REDIS_ADDR", server.Addr())
		t.Setenv("REDIS_DB", "3")
		t.Setenv("REDIS_PASSWORD", "secret")
		client, err := Open(context.Background())
		if err != nil { t.Fatal(err) }
		defer client.Close()
		if client.Options().DB != 3 { t.Fatal("REDIS_DB not applied") }
	})
	t.Run("invalid_db", func(t *testing.T) {
		for _, value := range []string{"-1", "not-a-number"} {
			t.Setenv("REDIS_DB", value)
			if _, err := Open(context.Background()); err == nil { t.Fatal("invalid DB accepted") }
		}
	})
}
