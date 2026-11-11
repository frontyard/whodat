package protocol

import "testing"

// TestStampedBus: events published through an account's stamped bus carry
// the acct; the shared bus leaves it empty (UI-originated refreshes).
func TestStampedBus(t *testing.T) {
	b := NewBus()
	ch, unsub := b.Subscribe(4)
	defer unsub()

	b.For("wa").Publish(Event{Kind: EvChatUpdated, ChatJID: "x@s.whatsapp.net"})
	b.Publish(Event{Kind: EvChatUpdated, ChatJID: "y@s.whatsapp.net"})

	e := <-ch
	if e.Acct != "wa" {
		t.Fatalf("stamped acct: %q", e.Acct)
	}
	e = <-ch
	if e.Acct != "" {
		t.Fatalf("plain publish stamped acct: %q", e.Acct)
	}
}

// TestSubscribeDrop: a slow subscriber drops rather than blocking publish.
func TestSubscribeDrop(t *testing.T) {
	b := NewBus()
	ch, unsub := b.Subscribe(1)
	defer unsub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			b.Publish(Event{Kind: EvChatUpdated})
		}
		close(done)
	}()
	<-done
	<-ch // one buffered event survived; the rest dropped without deadlock
}
