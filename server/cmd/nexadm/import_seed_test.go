package main

import (
	"context"
	"errors"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

func TestImportCommandRegistered(t *testing.T) {
	if commands["import"] == nil {
		t.Fatal("import command is not registered")
	}
}

func TestImportCommandRefusesProduction(t *testing.T) {
	t.Setenv("NEX_ENVIRONMENT", "production")
	err := runImport(context.Background(), []string{"--file", "src/data"})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != "forbidden" {
		t.Fatalf("err %#v", err)
	}
}
