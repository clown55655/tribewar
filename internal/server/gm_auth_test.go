package server

import (
	"context"
	"testing"
)

func TestLoadGMAdminUserIDsMergesConfigAndEnv(t *testing.T) {
	t.Setenv("TRIBEWAY_TEST_GM_ADMINS", "1002, 1003, invalid, 0")

	admins := loadGMAdminUserIDs([]uint64{1001}, "TRIBEWAY_TEST_GM_ADMINS")

	for _, userID := range []uint64{1001, 1002, 1003} {
		if _, ok := admins[userID]; !ok {
			t.Fatalf("expected user %d to be a GM admin", userID)
		}
	}
	if _, ok := admins[0]; ok {
		t.Fatal("zero user id must not be accepted as a GM admin")
	}
}

func TestAuthorizeGMUserDeniesUnconfiguredUser(t *testing.T) {
	service := &GMService{adminUserIDs: map[uint64]struct{}{1001: {}}}

	if resp := service.authorizeGMUser(1002); resp == nil {
		t.Fatal("expected non-admin user to be denied")
	}
	if resp := service.authorizeGMUser(1001); resp != nil {
		t.Fatalf("expected configured admin to be allowed, got %+v", resp)
	}
}

func TestContextUserID(t *testing.T) {
	ctx := context.WithValue(context.Background(), "user_id", "1001")
	userID, ok := contextUserID(ctx)
	if !ok || userID != 1001 {
		t.Fatalf("expected user id 1001 from context, got %d ok=%v", userID, ok)
	}
}
