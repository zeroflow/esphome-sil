package events

import (
	"errors"
	"sync"
	"testing"
)

func drain(s *Sub, n int) []Event {
	var out []Event
	for i := 0; i < n; i++ {
		select {
		case ev, ok := <-s.C():
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
	return out
}

func TestPublishAssignsIncreasingSeq(t *testing.T) {
	b := NewBroker()
	for want := uint64(1); want <= 3; want++ {
		if got := b.Publish(Event{K: "duty"}); got != want {
			t.Fatalf("Publish #%d returned seq %d", want, got)
		}
	}
	if b.Seq() != 3 {
		t.Fatalf("Seq() = %d, want 3", b.Seq())
	}
	s, err := b.Subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Start() != 3 {
		t.Fatalf("Start() = %d, want 3 (the seq at the moment of subscription)", s.Start())
	}
	b.Publish(Event{K: "duty", V: 1.0})
	ev := <-s.C()
	if ev.Seq != 4 || ev.K != "duty" {
		t.Fatalf("got %+v, want seq 4 duty", ev)
	}
}

func TestSlowSubscriberNeverBlocksAndLagsOnce(t *testing.T) {
	b := newBroker(4)
	slow, _ := b.Subscribe(nil)
	fast, _ := b.Subscribe(nil)
	for i := 0; i < 4; i++ {
		b.Publish(Event{K: "duty"})
		<-fast.C()
	}
	// slow never read: its buffer (4) is full. Five more are dropped; Publish must not block.
	for i := 0; i < 5; i++ {
		b.Publish(Event{K: "rpm"})
		<-fast.C()
	}
	if got := drain(slow, 2); len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("drained %+v, want the first two events", got)
	}
	// Room for two (the lagged marker and the event): 4 buffered, 2 read, 2 free.
	b.Publish(Event{K: "button", ID: "usr1", V: true}) // seq 10
	rest := drain(slow, 10)
	if len(rest) != 4 {
		t.Fatalf("got %d events after the drain, want 2 buffered + lagged + event: %+v", len(rest), rest)
	}
	if rest[2].K != "lagged" || rest[2].Dropped != 5 {
		t.Fatalf("rest[2] = %+v, want one lagged marker with dropped 5", rest[2])
	}
	if rest[3].K != "button" || rest[3].Seq != 10 {
		t.Fatalf("rest[3] = %+v, want the button event seq 10 right after the marker", rest[3])
	}
	if !(rest[1].Seq < rest[2].Seq && rest[2].Seq < rest[3].Seq) {
		t.Fatalf("seqs must keep increasing across the marker: %+v", rest)
	}
}

func TestKindFilterNeverDropsLagged(t *testing.T) {
	b := newBroker(2)
	s, err := b.Subscribe([]string{"duty"})
	if err != nil {
		t.Fatal(err)
	}
	b.Publish(Event{K: "rpm"})
	b.Publish(Event{K: "leds"})
	if got := drain(s, 5); len(got) != 0 {
		t.Fatalf("filtered kinds were delivered: %+v", got)
	}
	b.Publish(Event{K: "duty"})
	b.Publish(Event{K: "duty"})
	b.Publish(Event{K: "duty"}) // dropped (buffer 2)
	got := drain(s, 5)
	if len(got) != 2 || got[0].K != "duty" || got[1].K != "duty" {
		t.Fatalf("got %+v, want two duty events", got)
	}
	b.Publish(Event{K: "duty"})
	got = drain(s, 5)
	if len(got) != 2 || got[0].K != "lagged" || got[0].Dropped != 1 || got[1].K != "duty" {
		t.Fatalf("got %+v, want lagged(1) then duty", got)
	}
}

func TestUnknownKind(t *testing.T) {
	if k := UnknownKind([]string{"duty", "nope", "x"}); k != "nope" {
		t.Fatalf("UnknownKind = %q, want nope", k)
	}
	if k := UnknownKind(Kinds); k != "" {
		t.Fatalf("UnknownKind(Kinds) = %q, want none", k)
	}
	b := NewBroker()
	if _, err := b.Subscribe([]string{"nope"}); err == nil {
		t.Fatal("Subscribe accepted an unknown kind")
	}
}

func TestCloseClosesSubscribers(t *testing.T) {
	b := NewBroker()
	s1, _ := b.Subscribe(nil)
	s2, _ := b.Subscribe(nil)
	b.Publish(Event{K: "duty"})
	b.Close()
	b.Close() // idempotent
	for _, s := range []*Sub{s1, s2} {
		if ev, ok := <-s.C(); !ok || ev.K != "duty" {
			t.Fatalf("a buffered event must survive Close: %+v %v", ev, ok)
		}
		if _, ok := <-s.C(); ok {
			t.Fatal("channel still open after Close")
		}
	}
	if _, err := b.Subscribe(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("Subscribe after Close: %v, want ErrClosed", err)
	}
	b.Publish(Event{K: "duty"}) // must not panic
	s1.Close()                  // after Broker.Close: no-op
}

func TestSubscriberCap(t *testing.T) {
	b := NewBroker()
	var subs []*Sub
	for i := 0; i < MaxSubscribers; i++ {
		s, err := b.Subscribe(nil)
		if err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
		subs = append(subs, s)
	}
	if _, err := b.Subscribe(nil); !errors.Is(err, ErrTooMany) {
		t.Fatalf("the %dth Subscribe: %v, want ErrTooMany", MaxSubscribers+1, err)
	}
	subs[3].Close()
	subs[3].Close() // twice is fine
	if _, ok := <-subs[3].C(); ok {
		t.Fatal("a closed subscriber's channel is still open")
	}
	if _, err := b.Subscribe(nil); err != nil {
		t.Fatalf("Subscribe after a slot was freed: %v", err)
	}
}

func TestConcurrentUse(t *testing.T) {
	b := NewBroker()
	var wg sync.WaitGroup
	for p := 0; p < 4; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				b.Publish(Event{K: "rpm", Port: 1, V: float64(i)})
			}
		}()
	}
	for c := 0; c < 4; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				s, err := b.Subscribe([]string{"rpm"})
				if err != nil {
					continue
				}
				drain(s, 10)
				s.Close()
			}
		}()
	}
	wg.Wait()
	if b.Seq() != 8000 {
		t.Fatalf("Seq() = %d, want 8000", b.Seq())
	}
}
