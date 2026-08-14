package core

import (
	"testing"
	"time"
)

func TestSessionManagerLifecycle(t *testing.T) {
	manager := newSessionManager(time.Hour)
	token, expires, err := manager.Create("user-1")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || !expires.After(time.Now()) {
		t.Fatal("session token or expiration is invalid")
	}
	if userID, ok := manager.Resolve(token); !ok || userID != "user-1" {
		t.Fatalf("Resolve() = %q, %v", userID, ok)
	}
	manager.Delete(token)
	if _, ok := manager.Resolve(token); ok {
		t.Fatal("deleted session should not resolve")
	}
}

func TestLoginLimiterBlocksAfterFiveFailures(t *testing.T) {
	limiter := newLoginLimiter()
	for i := 0; i < 5; i++ {
		if !limiter.Allow("127.0.0.1") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
		limiter.Failure("127.0.0.1")
	}
	if limiter.Allow("127.0.0.1") {
		t.Fatal("sixth attempt should be blocked")
	}
	limiter.Success("127.0.0.1")
	if !limiter.Allow("127.0.0.1") {
		t.Fatal("successful login should reset limiter")
	}
}

func TestActiveSessionRegistryTerminatesRegisteredSession(t *testing.T) {
	registry := newActiveSessionRegistry()
	called := false
	registry.Add("session-1", "asset-1", func() { called = true })
	if !registry.Terminate("session-1") || !called {
		t.Fatal("registered session was not terminated")
	}
	registry.Remove("session-1")
	if registry.Terminate("session-1") {
		t.Fatal("removed session should not terminate")
	}
}

func TestActiveSessionRegistryTerminatesAssetSessions(t *testing.T) {
	registry := newActiveSessionRegistry()
	terminated := 0
	registry.Add("session-1", "asset-1", func() { terminated++ })
	registry.Add("session-2", "asset-1", func() { terminated++ })
	registry.Add("session-3", "asset-2", func() { terminated += 100 })
	ids := registry.TerminateAsset("asset-1")
	if len(ids) != 2 || terminated != 2 {
		t.Fatalf("TerminateAsset() ids=%v terminated=%d", ids, terminated)
	}
}
