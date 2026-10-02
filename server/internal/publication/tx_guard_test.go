package publication

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTxMethodsDoNotOwnTheTransaction(t *testing.T) {
	root := filepath.Join(".", "")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{".Begin(", ".Commit(", ".Rollback(", ".Within(", "context.WithValue"}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, needle := range banned {
			if strings.Contains(text, needle) {
				t.Fatalf("%s contains %s", name, needle)
			}
		}
	}
}
