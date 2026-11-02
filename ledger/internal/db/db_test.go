package db

import (
	"net/url"
	"testing"
)

func TestDSN(t *testing.T) {
	t.Run("url_priority", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://custom:secret@localhost/custom?sslmode=require")
		t.Setenv("DB_HOST", "ignored")
		if DSN() != "postgres://custom:secret@localhost/custom?sslmode=require" { t.Fatal("DATABASE_URL did not take precedence") }
	})
	t.Run("defaults", func(t *testing.T) {
		for _, key := range []string{"DATABASE_URL", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASS", "DB_NAME", "DB_SSLMODE"} { t.Setenv(key, "") }
		if DSN() != "postgres://postgres:postgres@localhost:5432/cashapp?sslmode=disable" { t.Fatal(DSN()) }
	})
	t.Run("escaped_credentials", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("DB_USER", "user@local")
		t.Setenv("DB_PASS", "p@ss:/?#")
		u, err := url.Parse(DSN())
		if err != nil { t.Fatal(err) }
		pass, _ := u.User.Password()
		if u.User.Username() != "user@local" || pass != "p@ss:/?#" { t.Fatal("credentials were not escaped") }
	})
}
