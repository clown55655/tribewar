package rpc

import (
	"testing"
	"time"
)

func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	breaker := NewCircuitBreaker(2, 10*time.Millisecond)

	if !breaker.Allow() {
		t.Fatal("new breaker should allow requests")
	}
	breaker.RecordFailure()
	if breaker.State() != CircuitClosed {
		t.Fatalf("expected closed after one failure, got %s", breaker.State())
	}
	breaker.RecordFailure()
	if breaker.State() != CircuitOpen {
		t.Fatalf("expected open after threshold, got %s", breaker.State())
	}
	if breaker.Allow() {
		t.Fatal("open breaker should reject before timeout")
	}
	time.Sleep(15 * time.Millisecond)
	if !breaker.Allow() {
		t.Fatal("breaker should allow half-open probe after timeout")
	}
	breaker.RecordSuccess()
	if breaker.State() != CircuitClosed {
		t.Fatalf("expected closed after success, got %s", breaker.State())
	}
}

func TestConnectionPoolStats(t *testing.T) {
	pool := NewRPCConnectionPool("127.0.0.1", 9001, 2)
	stats := pool.Stats()
	if stats.Address != "127.0.0.1" || stats.Port != 9001 || stats.MaxSize != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if err := pool.HealthCheck(); err != nil {
		t.Fatalf("expected empty pool with capacity to be available: %v", err)
	}
}
