package adminauth

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

func TestNormalizeUsername(t *testing.T) {
	got, err := NormalizeUsername(" Ａdmin ")
	if err != nil || got != "admin" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := NormalizeUsername("ab"); err == nil {
		t.Fatal("short username")
	}
	if _, err := NormalizeUsername("-admin"); err == nil {
		t.Fatal("leading punctuation")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("password"); err == nil || !strings.Contains(err.Error(), "常见") {
		t.Fatal(err)
	}
	if err := ValidatePassword("short-pass"); err == nil || !strings.Contains(err.Error(), "12") {
		t.Fatal(err)
	}
	if err := ValidatePassword(strings.Repeat("a", 73)); err == nil {
		t.Fatal("long password")
	}
	if err := ValidatePassword("correct-horse-1"); err != nil {
		t.Fatal(err)
	}
}

func TestArgon2PHC(t *testing.T) {
	salt := bytes.Repeat([]byte{2}, 16)
	password := "correct-horse-1"
	key := argon2.IDKey([]byte(password), salt, 1, 32, 1, 32)
	encoded := formatPHC(32, 1, 1, salt, key)
	ok, err := Argon2Hasher{}.Verify(encoded, password)
	if err != nil || !ok {
		t.Fatalf("verify ok=%v err=%v", ok, err)
	}
	ok, err = Argon2Hasher{}.Verify(encoded, "correct-horse-2")
	if err != nil || ok {
		t.Fatalf("mismatch ok=%v err=%v", ok, err)
	}
	if _, err := (Argon2Hasher{}).Verify("not-a-hash", password); err == nil {
		t.Fatal("malformed hash")
	}
}

func TestArgon2ProductionParams(t *testing.T) {
	encoded, err := Argon2Hasher{}.Hash("correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatal(encoded)
	}
	ok, err := Argon2Hasher{}.Verify(encoded, "correct-horse-1")
	if err != nil || !ok {
		t.Fatalf("verify ok=%v err=%v", ok, err)
	}
	if strings.Contains(encoded, "correct-horse-1") {
		t.Fatal("hash contains password")
	}
}

func TestSanitizeDropsSecretsAndLongText(t *testing.T) {
	long := strings.Repeat("文", 501)
	raw := []byte(`{"title":{"from":"a","to":"b"},"password":{"from":"x","to":"y"},"note":{"from":"","to":"` + long + `"}}`)
	got, err := Sanitize(raw)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]map[string]string
	if err := json.Unmarshal(got, &obj); err != nil {
		t.Fatal(err, string(got))
	}
	if _, ok := obj["password"]; ok {
		t.Fatal("password kept")
	}
	if obj["title"]["to"] != "b" || obj["note"]["to"] != "omitted" {
		t.Fatalf("%s", got)
	}
	exact := strings.Repeat("文", 500)
	kept, err := Sanitize([]byte(`{"note":{"to":"` + exact + `"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(kept, []byte(exact)) {
		t.Fatal("boundary omitted")
	}
}

func TestHashJSONCanonical(t *testing.T) {
	a, err := HashJSON([]byte("{\n \"b\": 1, \"a\": {\"d\": 2, \"c\": \"x&y\"}\n}"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashJSON([]byte(`{"a":{"c":"x&y","d":2},"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("%s vs %s", a, b)
	}
	left, _ := HashJSON([]byte(`{"a":[1,2]}`))
	right, _ := HashJSON([]byte(`{"a":[2,1]}`))
	if left == right {
		t.Fatal("array order ignored")
	}
	empty, err := HashJSON(nil)
	if err != nil || empty == "" {
		t.Fatal(empty, err)
	}
}

func TestLoginLimiterFixedWindow(t *testing.T) {
	lim := NewMemoryLimiter()
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < loginUserLimit; i++ {
		if _, blocked := lim.TooMany("ada", "10.0.0.1", now); blocked {
			t.Fatal("blocked early")
		}
		lim.Fail("ada", "10.0.0.1", now)
	}
	retry, blocked := lim.TooMany("ada", "10.0.0.8", now)
	if !blocked || retry < 1 {
		t.Fatalf("retry %d blocked %v", retry, blocked)
	}
	lim.Success("ada")
	if _, blocked := lim.TooMany("ada", "10.0.0.8", now); blocked {
		t.Fatal("success did not clear username")
	}
	for i := 0; i < loginIPLimit; i++ {
		lim.Fail("other", "10.0.0.9", now)
	}
	if _, blocked := lim.TooMany("fresh", "10.0.0.9", now); !blocked {
		t.Fatal("ip limit")
	}
	later := now.Add(loginWindow)
	if _, blocked := lim.TooMany("fresh", "10.0.0.9", later); blocked {
		t.Fatal("window did not reset")
	}
}

func TestExecuteValidation(t *testing.T) {
	ex := &WriteExecutor{}
	fn := func(context.Context, pgx.Tx) (ports.WriteResult, error) {
		t.Fatal("fn ran")
		return ports.WriteResult{}, nil
	}
	principal := PrincipalKey(catalog.AdminID(uuid.New()))
	_, err := ex.Execute(t.Context(), "admin:not-a-uuid", "POST /api/admin/resources", "key", "hash", fn)
	if err == nil {
		t.Fatal("principal")
	}
	_, err = ex.Execute(t.Context(), principal, "ingest.batch.v1", "key", "hash", fn)
	if err == nil || !strings.Contains(err.Error(), "采集") {
		t.Fatal(err)
	}
	_, err = ex.Execute(t.Context(), principal, "POST /api/admin/resources", "bad\nkey", "hash", fn)
	if err == nil {
		t.Fatal("key")
	}
	_, err = ex.Execute(t.Context(), principal, "POST /api/admin/resources", "key", "hash", fn)
	var ae *apperr.Error
	if err == nil || !strings.Contains(err.Error(), "写执行器") {
		t.Fatalf("%v", err)
	}
	_ = ae
}
