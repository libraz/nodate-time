package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("correct horse battery staple", h) {
		t.Fatal("correct password rejected")
	}
	if CheckPassword("wrong", h) {
		t.Fatal("wrong password accepted")
	}
	// A bcrypt hash, which is what the previous implementation wrote, must
	// not verify: it is the shape the contract says will not be there.
	if CheckPassword("x", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy") {
		t.Fatal("non-argon2id hash accepted")
	}
	if CheckPassword("x", "garbage") {
		t.Fatal("malformed hash accepted")
	}
}
