package db

import (
	"os"
	"strings"
	"testing"
)

func TestPhase1SchemaMatchesGooseUp(t *testing.T) {
	schema, err := os.ReadFile("schema/phase1.sql")
	if err != nil {
		t.Fatal(err)
	}
	mig, err := os.ReadFile("migrations/00002_phase1.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(mig)
	const upMark = "-- +goose Up\n"
	const downMark = "\n-- +goose Down\n"
	start := strings.Index(text, upMark)
	end := strings.Index(text, downMark)
	if start < 0 || end < 0 || end < start {
		t.Fatal("goose up/down markers missing")
	}
	got := strings.TrimSpace(text[start+len(upMark) : end])
	want := strings.TrimSpace(string(schema))
	if got != want {
		t.Fatal("db/schema/phase1.sql drifted from the goose up section")
	}
}

func TestPhase2SchemaMatchesGooseUp(t *testing.T) {
	schema, err := os.ReadFile("schema/phase2.sql")
	if err != nil {
		t.Fatal(err)
	}
	mig, err := os.ReadFile("migrations/00004_phase2.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(mig)
	const upMark = "-- +goose Up\n"
	const downMark = "\n-- +goose Down\n"
	start := strings.Index(text, upMark)
	end := strings.Index(text, downMark)
	if start < 0 || end < 0 || end < start {
		t.Fatal("goose up/down markers missing")
	}
	got := strings.TrimSpace(text[start+len(upMark) : end])
	want := strings.TrimSpace(string(schema))
	if got != want {
		t.Fatal("db/schema/phase2.sql drifted from the goose up section")
	}
}

func TestCollectorSchemaMatchesMigration(t *testing.T) {
	schema, err := os.ReadFile("schema/phase3.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("migrations/00005_collectors.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Split(strings.TrimPrefix(string(migration), "-- +goose Up\n"), "\n-- +goose Down\n")[0]
	if strings.TrimSpace(text) != strings.TrimSpace(string(schema)) {
		t.Fatal("collector schema drift")
	}
}

func TestAssistantSchemaMatchesMigration(t *testing.T) {
	schema, err := os.ReadFile("schema/phase4.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("migrations/00006_assistant.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.Split(strings.TrimPrefix(string(migration), "-- +goose Up\n"), "\n-- +goose Down\n")[0]
	if strings.TrimSpace(up) != strings.TrimSpace(string(schema)) {
		t.Fatal("assistant schema drift")
	}
}
