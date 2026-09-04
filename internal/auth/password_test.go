package auth

import "testing"

func TestValidatePasswordLength(t *testing.T) {
	if err := validatePasswordLength("012345678901"); err != nil { // 12 chars
		t.Fatalf("12-char password rejected: %v", err)
	}
	if err := validatePasswordLength("01234567890"); err == nil { // 11 chars
		t.Fatal("11-char password accepted, want rejection")
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatalf("hashPassword() error = %v", err)
	}
	if !checkPassword(hash, testPassword) {
		t.Fatal("checkPassword() = false for the correct password")
	}
	if checkPassword(hash, "wrong password entirely") {
		t.Fatal("checkPassword() = true for an incorrect password")
	}
}
