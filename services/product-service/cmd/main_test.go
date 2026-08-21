package main

import (
	"strings"
	"testing"
)

func TestCatalogDSNUsesCatalogDatabase(t *testing.T) {
	got, err := catalogDSN("app:secret@tcp(localhost:3306)/user_db?parseTime=true")
	if err != nil || !strings.Contains(got, "/catalog_db?") {
		t.Fatalf("dsn=%q err=%v", got, err)
	}
}
