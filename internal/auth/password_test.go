package auth

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password was not hashed")
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if CheckPassword(hash, "wrong password") {
		t.Fatal("invalid password accepted")
	}
}
