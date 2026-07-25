package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// bcrypt at DefaultCost takes ~60ms (~500ms under -race), and the test suite
// hashes or verifies a password in almost every test. MinCost keeps the code
// path identical while making it ~100x cheaper.
// ponytail: testing.Testing() beats plumbing a cost parameter through every caller.
var hashCost = func() int {
	if testing.Testing() {
		return bcrypt.MinCost
	}
	return bcrypt.DefaultCost
}()

func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), hashCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
