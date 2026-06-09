package auth

import (
	"testing"
)

func TestHashPassword(t *testing.T) {
	hash, err := hashPassword("testpassword")
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("hashPassword returned empty hash")
	}
	if hash == "testpassword" {
		t.Fatal("hashPassword returned plaintext password")
	}
}

func TestCheckPassword_Valid(t *testing.T) {
	hash, err := hashPassword("mypassword")
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}
	if !checkPassword(hash, "mypassword") {
		t.Fatal("checkPassword should return true for correct password")
	}
}

func TestCheckPassword_Invalid(t *testing.T) {
	hash, err := hashPassword("mypassword")
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}
	if checkPassword(hash, "wrongpassword") {
		t.Fatal("checkPassword should return false for incorrect password")
	}
}

func TestCheckPassword_EmptyPassword(t *testing.T) {
	hash, err := hashPassword("mypassword")
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}
	if checkPassword(hash, "") {
		t.Fatal("checkPassword should return false for empty password")
	}
}

func TestHashPassword_DifferentHashes(t *testing.T) {
	hash1, _ := hashPassword("samepassword")
	hash2, _ := hashPassword("samepassword")
	if hash1 == hash2 {
		t.Fatal("bcrypt should produce different hashes for same input (different salts)")
	}
	// Both should still validate
	if !checkPassword(hash1, "samepassword") {
		t.Fatal("hash1 should validate")
	}
	if !checkPassword(hash2, "samepassword") {
		t.Fatal("hash2 should validate")
	}
}
