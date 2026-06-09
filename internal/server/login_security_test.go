package server

import (
	"testing"
	"time"
)

func TestLoginPasswordHashUsesBcrypt(t *testing.T) {
	server := &LoginServer{}
	service := NewLoginService(server)

	hash, err := service.hashPassword("secret")
	if err != nil {
		t.Fatalf("hashPassword error: %v", err)
	}
	if !isBcryptHash(hash) {
		t.Fatalf("expected bcrypt hash, got %q", hash)
	}

	ok, needsRehash := service.verifyPassword("secret", hash)
	if !ok {
		t.Fatal("expected bcrypt password to verify")
	}
	if needsRehash {
		t.Fatal("bcrypt password should not need rehash")
	}
}

func TestLoginPasswordAllowsLegacyMD5AndRequiresRehash(t *testing.T) {
	server := &LoginServer{}
	service := NewLoginService(server)

	legacyHash := service.legacyMD5PasswordHash("secret")
	ok, needsRehash := service.verifyPassword("secret", legacyHash)
	if !ok {
		t.Fatal("expected legacy md5 password to verify")
	}
	if !needsRehash {
		t.Fatal("legacy md5 password should need rehash")
	}
}

func TestLoginTokenUsesConfiguredSecret(t *testing.T) {
	server := &LoginServer{
		tokenSecret: []byte("test-secret"),
		tokenExpiry: time.Hour,
	}
	service := NewLoginService(server)

	token := service.generateToken(123)
	claims, err := service.validateToken(token)
	if err != nil {
		t.Fatalf("validateToken error: %v", err)
	}
	if claims.userID != 123 {
		t.Fatalf("user id mismatch: got %d want 123", claims.userID)
	}

	server.tokenSecret = []byte("other-secret")
	if _, err := service.validateToken(token); err == nil {
		t.Fatal("expected token validation to fail with different secret")
	}
}
