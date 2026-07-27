package auth

import "golang.org/x/crypto/bcrypt"

// bcryptCost trades hashing latency for brute-force resistance. 12 is the
// current reasonable floor for an interactive login; raising it is a config
// change for a future release, not something worth a migration today.
const bcryptCost = 12

// HashPassword returns a hash safe to store. The plaintext is never retained.
func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether plaintext matches the stored hash. It never
// distinguishes "wrong password" from "unknown user" to the caller — that
// distinction is what lets a login handler return one message for both,
// which is the point (an attacker should not learn which emails exist).
func VerifyPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}
