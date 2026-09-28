package bus

import "testing"

func TestFanOutAndDrop(t *testing.T) {
	b := New[int](1)
	c1, cancel1 := b.Subscribe()
	c2, cancel2 := b.Subscribe()
	defer cancel2()
	if n := b.Publish(1); n != 2 {
		t.Fatalf("want 2 deliveries, got %d", n)
	}
	// c1 buffer full now; second publish must not block and should drop for c1.
	if n := b.Publish(2); n != 0 {
		t.Fatalf("want 0 deliveries when buffers full, got %d", n)
	}
	if v := <-c1; v != 1 {
		t.Fatalf("want 1, got %d", v)
	}
	if v := <-c2; v != 1 {
		t.Fatalf("want 1, got %d", v)
	}
	cancel1()
	cancel1() // idempotent
	if b.Len() != 1 {
		t.Fatalf("want 1 subscriber, got %d", b.Len())
	}
	if _, ok := <-c1; ok {
		t.Fatal("expected closed channel")
	}
}
