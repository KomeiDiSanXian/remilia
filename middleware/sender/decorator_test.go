package sender

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KomeiDiSanXian/remilia/platform"
)

type mockSender struct {
	failCount int
	callCount atomic.Int32
}

func (m *mockSender) Send(_ context.Context, _ platform.SendRequest) (platform.SendResult, error) {
	m.callCount.Add(1)
	if m.failCount > 0 {
		m.failCount--
		return platform.SendResult{}, errors.New("mock failure")
	}
	return platform.SendResult{MessageID: "mock-msg-id"}, nil
}

func TestChain(t *testing.T) {
	var order []string
	d1 := func(next platform.Sender) platform.Sender {
		return &orderRecorder{next: next, name: "d1", order: &order}
	}
	d2 := func(next platform.Sender) platform.Sender {
		return &orderRecorder{next: next, name: "d2", order: &order}
	}

	chained := Chain(d1, d2)(&mockSender{})
	_, _ = chained.Send(context.Background(), platform.SendRequest{})

	// Chain(a,b)(s) == a(b(s)), so d1 is outermost (executed first on way in)
	if len(order) != 2 || order[0] != "d1" || order[1] != "d2" {
		t.Fatalf("unexpected order: %v", order)
	}
}

type orderRecorder struct {
	next  platform.Sender
	name  string
	order *[]string
}

func (r *orderRecorder) Send(ctx context.Context, req platform.SendRequest) (platform.SendResult, error) {
	*r.order = append(*r.order, r.name)
	return r.next.Send(ctx, req)
}

func TestRetrySuccess(t *testing.T) {
	mock := &mockSender{failCount: 2}
	s := Retry(3, time.Millisecond)(mock)

	res, err := s.Send(context.Background(), platform.SendRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.MessageID != "mock-msg-id" {
		t.Fatalf("expected mock-msg-id, got %s", res.MessageID)
	}
	if n := mock.callCount.Load(); n != 3 {
		t.Fatalf("expected 3 calls, got %d", n)
	}
}

func TestRetryExhausted(t *testing.T) {
	mock := &mockSender{failCount: 5}
	s := Retry(3, time.Millisecond)(mock)

	_, err := s.Send(context.Background(), platform.SendRequest{})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if n := mock.callCount.Load(); n != 3 {
		t.Fatalf("expected 3 calls, got %d", n)
	}
}

func TestRetryContextCancel(t *testing.T) {
	mock := &mockSender{failCount: 100}
	s := Retry(5, 100*time.Millisecond)(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Send(ctx, platform.SendRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected Canceled, got %v", err)
	}
}

func TestRetryZeroAttempts(t *testing.T) {
	mock := &mockSender{failCount: 1}
	s := Retry(1, time.Millisecond)(mock)

	_, err := s.Send(context.Background(), platform.SendRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mock := &mockSender{}
		slow := func(next platform.Sender) platform.Sender {
			return &slowSender{next: next, delay: 200 * time.Millisecond}
		}

		s := Timeout(50 * time.Millisecond)(slow(mock))

		_, err := s.Send(context.Background(), platform.SendRequest{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded, got %v", err)
		}
	})
}

type slowSender struct {
	next  platform.Sender
	delay time.Duration
}

func (s *slowSender) Send(ctx context.Context, req platform.SendRequest) (platform.SendResult, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return platform.SendResult{}, ctx.Err()
	}
	return s.next.Send(ctx, req)
}

func TestRetryWithTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Retry(Timeout(Sender)) — 每次重试都有独立的超时
		mock := &mockSender{}
		slow := &slowSender{next: mock, delay: 200 * time.Millisecond}

		s := Retry(3, time.Millisecond)(
			Timeout(50 * time.Millisecond)(slow),
		)

		_, err := s.Send(context.Background(), platform.SendRequest{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded after retries, got %v", err)
		}
		// Should have attempted 3 times, each timing out
		if n := mock.callCount.Load(); n != 0 {
			t.Fatalf("expected 0 successful calls (all timed out), got %d", n)
		}
	})
}

func TestMetricsDecorator(t *testing.T) {
	mock := &mockSender{}
	s := Metrics("test")(mock)

	_, err := s.Send(context.Background(), platform.SendRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoggingDecorator(t *testing.T) {
	mock := &mockSender{}
	s := Logging()(mock)

	_, err := s.Send(context.Background(), platform.SendRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
