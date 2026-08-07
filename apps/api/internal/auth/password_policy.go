package auth

import (
	"fmt"
	"unicode/utf8"
)

// PasswordMinLength and PasswordMaxLength are the bounds a password answers to
// however it is set. They count characters rather than bytes, because that is
// what JSON Schema's minLength/maxLength count and the API declares them as
// tags on its request bodies. A struct tag cannot hold a constant, so those
// tags are a second copy of these numbers and have to be moved with them.
const (
	PasswordMinLength = 8
	PasswordMaxLength = 128
)

// ValidatePassword applies those bounds where no request schema does it, so a
// password accepted outside the API is one the API would also have accepted.
func ValidatePassword(password string) error {
	switch n := utf8.RuneCountInString(password); {
	case n < PasswordMinLength:
		return fmt.Errorf("password must be at least %d characters", PasswordMinLength)
	case n > PasswordMaxLength:
		return fmt.Errorf("password must be at most %d characters", PasswordMaxLength)
	}
	return nil
}
