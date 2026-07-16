package domain

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for the 2FA registration PIN
// (security-architecture §2: m=64MB, t=3, p=4).
const (
	pinArgonTime    uint32 = 3
	pinArgonMemory  uint32 = 64 * 1024 // KiB
	pinArgonThreads uint8  = 4
	pinArgonKeyLen  uint32 = 32
	pinSaltLen             = 16
	PINMinLength           = 4
)

// HashPIN returns a PHC-formatted argon2id hash
// ($argon2id$v=19$m=…,t=…,p=…$salt$key).
func HashPIN(pin string, entropy io.Reader) (string, error) {
	if len(pin) < PINMinLength {
		return "", fmt.Errorf("pin must be at least %d characters", PINMinLength)
	}
	salt := make([]byte, pinSaltLen)
	if _, err := io.ReadFull(entropy, salt); err != nil {
		return "", fmt.Errorf("pin salt: %w", err)
	}
	key := argon2.IDKey([]byte(pin), salt, pinArgonTime, pinArgonMemory, pinArgonThreads, pinArgonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, pinArgonMemory, pinArgonTime, pinArgonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPIN verifies pin against a PHC argon2id string. Parameters are read
// from the hash itself so stored hashes keep verifying after we tune the
// constants (upgrade-on-login can rehash).
func CheckPIN(phc, pin string) (bool, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("pin hash: malformed PHC string")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("pin hash: version: %w", err)
	}
	if version != argon2.Version {
		return false, fmt.Errorf("pin hash: unsupported argon2 version %d", version)
	}
	var (
		mem     uint32
		times   uint32
		threads uint8
	)
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &times, &threads); err != nil {
		return false, fmt.Errorf("pin hash: params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("pin hash: salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("pin hash: key: %w", err)
	}
	got := argon2.IDKey([]byte(pin), salt, times, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
