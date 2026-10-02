package adminauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	argonSaltLen        = 16
	argonKeyLen  uint32 = 32
	// Caps stop a corrupted hash from allocating an unbounded amount of memory.
	argonMaxTime    uint32 = 8
	argonMaxMemory  uint32 = 256 * 1024
	argonMaxThreads uint8  = 4
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) (bool, error)
}

func ValidatePassword(password string) error {
	if commonPassword(password) {
		return apperr.Invalid("口令过于常见")
	}
	n := len(password)
	if n < 12 || n > 72 {
		return apperr.Invalid("口令长度必须在 12 到 72 字节之间")
	}
	return nil
}

func commonPassword(password string) bool {
	switch strings.ToLower(password) {
	case "password", "admin", "changeme", "nexclub":
		return true
	default:
		return false
	}
}

// Argon2Hasher stores argon2id parameters in the PHC string and reads them back on verify.
type Argon2Hasher struct{}

func (Argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return formatPHC(argonMemory, argonTime, argonThreads, salt, key), nil
}

func (Argon2Hasher) Verify(hash, password string) (bool, error) {
	memory, timeCost, threads, salt, want, err := parsePHC(hash)
	if err != nil {
		return false, err
	}
	if timeCost == 0 || timeCost > argonMaxTime || memory > argonMaxMemory || threads == 0 || threads > argonMaxThreads {
		return false, fmt.Errorf("password hash parameters")
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func formatPHC(memory, timeCost uint32, threads uint8, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, timeCost, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func parsePHC(hash string) (memory, timeCost uint32, threads uint8, salt, key []byte, err error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || !strings.HasPrefix(parts[2], "v=") {
		return 0, 0, 0, nil, nil, fmt.Errorf("password hash format")
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("password hash format")
	}
	var parsedThreads int
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &parsedThreads)
	if err != nil || parsedThreads < 1 || parsedThreads > 255 {
		return 0, 0, 0, nil, nil, fmt.Errorf("password hash format")
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return 0, 0, 0, nil, nil, fmt.Errorf("password hash format")
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return 0, 0, 0, nil, nil, fmt.Errorf("password hash format")
	}
	return memory, timeCost, uint8(parsedThreads), salt, key, nil
}

// FakeHasher is a deterministic test double. It must not be used outside tests.
type FakeHasher struct{}

func (FakeHasher) Hash(password string) (string, error) {
	return "fake$" + password, nil
}

func (FakeHasher) Verify(hash, password string) (bool, error) {
	want := "fake$" + password
	if len(hash) != len(want) {
		return false, nil
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(want)) == 1, nil
}
