package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// PASSWORD_HASH (when set in the environment) replaces the plaintext
// PASSWORD. The format is deliberately simple to generate and to inspect:
//
//	pbkdf2-sha256$<iterations>$<base64 salt>$<base64 digest>
//
// cmd/hashpw prints a ready-to-paste value.
const passwordHashPrefix = "pbkdf2-sha256$"

// defaultHashIterations follows the current OWASP recommendation for
// PBKDF2-HMAC-SHA256; a login costs a few hundred milliseconds, which is
// exactly the point.
const defaultHashIterations = 600000

// hashKeyLen is the digest length in bytes (256 bit).
const hashKeyLen = 32

// passwordVerifier is a parsed PASSWORD_HASH value. A nil *passwordVerifier
// means "no hash configured": authentication falls back to comparing the
// plaintext PASSWORD.
type passwordVerifier struct {
	iterations int
	salt       []byte
	hash       []byte
	// broken marks a hash that could not be parsed. It verifies nothing, so a
	// malformed PASSWORD_HASH fails closed instead of silently downgrading to
	// the plaintext PASSWORD (whose default is admin/admin).
	broken bool
}

// parsePasswordHash parses the pbkdf2-sha256$... format. Every error is a
// configuration problem the operator should see at startup.
func parsePasswordHash(encoded string) (*passwordVerifier, error) {
	rest, ok := strings.CutPrefix(encoded, passwordHashPrefix)
	if !ok {
		return nil, errors.New("hash must start with " + passwordHashPrefix)
	}
	iterStr, rest, ok := strings.Cut(rest, "$")
	if !ok {
		return nil, errors.New("hash is missing the iteration count")
	}
	iterations, err := strconv.Atoi(iterStr)
	if err != nil || iterations < 1 || iterations > 10_000_000 {
		return nil, errors.New("iteration count is missing or out of range")
	}
	saltStr, digestStr, ok := strings.Cut(rest, "$")
	if !ok {
		return nil, errors.New("hash is missing the digest")
	}
	salt, err := base64.StdEncoding.DecodeString(saltStr)
	if err != nil || len(salt) < 8 {
		return nil, errors.New("salt is missing, too short, or not base64")
	}
	digest, err := base64.StdEncoding.DecodeString(digestStr)
	if err != nil || len(digest) != hashKeyLen {
		return nil, errors.New("digest is not " + strconv.Itoa(hashKeyLen) + " base64 bytes")
	}
	return &passwordVerifier{iterations: iterations, salt: salt, hash: digest}, nil
}

// verify reports whether the password matches the stored digest. The compare
// is constant-time, like the plaintext path.
func (v *passwordVerifier) verify(password string) bool {
	if v.broken {
		return false
	}
	computed := pbkdf2SHA256([]byte(password), v.salt, v.iterations, hashKeyLen)
	return subtle.ConstantTimeCompare(computed, v.hash) == 1
}

// NewPasswordHash hashes a password into the PASSWORD_HASH format: a random
// 16-byte salt, the given number of iterations (defaultHashIterations when
// not positive), and the 32-byte digest. cmd/hashpw is a thin wrapper around
// this.
func NewPasswordHash(password string, iterations int) (string, error) {
	if iterations <= 0 {
		iterations = defaultHashIterations
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	digest := pbkdf2SHA256([]byte(password), salt, iterations, hashKeyLen)
	return fmt.Sprintf(
		"%s%d$%s$%s",
		passwordHashPrefix,
		iterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(digest),
	), nil
}

// pbkdf2SHA256 implements PBKDF2 (RFC 8018) over HMAC-SHA256. The stdlib has
// no PBKDF2 and this project deliberately takes no dependencies, so this is
// the standard construction; the tests pin it to published vectors.
func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	blocks := (keyLen + hashLen - 1) / hashLen
	dk := make([]byte, 0, blocks*hashLen)
	var blockIndex [4]byte
	u := make([]byte, hashLen)
	for block := 1; block <= blocks; block++ {
		prf.Reset()
		prf.Write(salt)
		blockIndex[0] = byte(block >> 24)
		blockIndex[1] = byte(block >> 16)
		blockIndex[2] = byte(block >> 8)
		blockIndex[3] = byte(block)
		prf.Write(blockIndex[:4])
		dk = prf.Sum(dk)
		t := dk[len(dk)-hashLen:]
		copy(u, t)
		for n := 2; n <= iterations; n++ {
			// U_n = HMAC(password, U_{n-1}): the previous round's output is
			// the message of this round.
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for i := range u {
				t[i] ^= u[i]
			}
		}
	}
	return dk[:keyLen]
}
