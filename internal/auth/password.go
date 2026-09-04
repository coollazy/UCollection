package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// minPasswordLength is the only password rule (技術架構設計第9節「帳號模型」):
// length only, no complexity requirements, front/back end consistent.
const minPasswordLength = 12

// bcryptCost matches 技術架構設計第9節.
const bcryptCost = 12

var errPasswordTooShort = errors.New("auth: password must be at least 12 characters")

func validatePasswordLength(pw string) error {
	if len(pw) < minPasswordLength {
		return errPasswordTooShort
	}
	return nil
}

func hashPassword(pw string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
