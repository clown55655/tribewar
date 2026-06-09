package actor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testActor struct {
	*BaseActor
	processed chan string
	active    int32
	maxActive int32
	sleep     time.Duration
}

func newTestActor(id string, mailboxSize int) *testActor {
	return &testActor{
		BaseActor: NewBaseActor(id, "test", mailboxSize),
		processed: make(chan string, mailboxSize),
	}
}

func (a *testActor) OnReceive(ctx context.Context, msg Message) error {
	current := atomic.AddInt32(&a.active, 1)
	for {
		max := atomic.LoadInt32(&a.maxActive)
		if current <= max || atomic.CompareAndSwapInt32(&a.maxActive, max, current) {
			break
		}
	}
	defer atomic.AddInt32(&a.active, -1)

	if a.sleep > 0 {
		time.Sleep(a.sleep)
	}
	a.processed <- msg.GetType()
	return nil
}

func (a *testActor) OnStart(ctx context.Context) error { return nil }
func (a *testActor) OnStop(ctx context.Context) error  { return nil }

func TestActorSystemTellUsesMailboxSequentially(t *testing.T) {
	sys := NewActorSystem("test")
	a := newTestActor("a1", 10)
	a.sleep = 5 * time.Millisecond

	if err := sys.SpawnActor(a); err != nil {
		t.Fatalf("spawn actor: %v", err)
	}

	for _, msgType := range []string{"one", "two", "three"} {
		if err := sys.Tell("a1", NewMessage(msgType, nil)); err != nil {
			t.Fatalf("tell %s: %v", msgType, err)
		}
	}

	for _, want := range []string{"one", "two", "three"} {
		select {
		case got := <-a.processed:
			if got != want {
				t.Fatalf("message order mismatch: got %s want %s", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for %s", want)
		}
	}

	if got := atomic.LoadInt32(&a.maxActive); got != 1 {
		t.Fatalf("actor handled messages concurrently, max active = %d", got)
	}

	if err := sys.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestActorSystemDifferentActorsRunInParallel(t *testing.T) {
	sys := NewActorSystem("test")
	a1 := newTestActor("a1", 1)
	a2 := newTestActor("a2", 1)
	a1.sleep = 100 * time.Millisecond
	a2.sleep = 100 * time.Millisecond

	if err := sys.SpawnActor(a1); err != nil {
		t.Fatalf("spawn a1: %v", err)
	}
	if err := sys.SpawnActor(a2); err != nil {
		t.Fatalf("spawn a2: %v", err)
	}

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := sys.Tell("a1", NewMessage("one", nil)); err != nil {
			t.Errorf("tell a1: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := sys.Tell("a2", NewMessage("two", nil)); err != nil {
			t.Errorf("tell a2: %v", err)
		}
	}()
	wg.Wait()

	for _, ch := range []chan string{a1.processed, a2.processed} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for actor")
		}
	}

	if elapsed := time.Since(start); elapsed >= 190*time.Millisecond {
		t.Fatalf("actors did not run in parallel, elapsed = %v", elapsed)
	}

	if err := sys.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestActorSystemTellAfterShutdownFails(t *testing.T) {
	sys := NewActorSystem("test")
	a := newTestActor("a1", 1)

	if err := sys.SpawnActor(a); err != nil {
		t.Fatalf("spawn actor: %v", err)
	}
	if err := sys.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if err := sys.Tell("a1", NewMessage("late", nil)); err == nil {
		t.Fatal("expected tell to stopped actor to fail")
	}
}

func TestActorStats(t *testing.T) {
	sys := NewActorSystem("test")
	a := newTestActor("a1", 10)
	if err := sys.SpawnActor(a); err != nil {
		t.Fatalf("spawn actor: %v", err)
	}
	if err := sys.Tell("a1", NewMessage("one", nil)); err != nil {
		t.Fatalf("tell: %v", err)
	}
	select {
	case <-a.processed:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for actor")
	}

	stats := sys.GetStats()
	if stats.TotalActors != 1 {
		t.Fatalf("expected one actor, got %d", stats.TotalActors)
	}
	if stats.TotalMessagesEnqueued != 1 || stats.TotalMessagesProcessed != 1 {
		t.Fatalf("unexpected message stats: %+v", stats)
	}
	if len(stats.Actors) != 1 || stats.Actors[0].MailboxCapacity != 10 {
		t.Fatalf("unexpected actor stats: %+v", stats.Actors)
	}

	if err := sys.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
