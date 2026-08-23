package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunConfirmationConsumerRestartsAfterTransientFailureAndStopsOnCancellation(t *testing.T) {
	consumer := &fakeConfirmationRunner{started: make(chan struct{}, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runConfirmationConsumer(ctx, consumer, time.Millisecond)
	}()

	for range 2 {
		select {
		case <-consumer.started:
		case <-time.After(time.Second):
			t.Fatal("confirmation consumer did not restart")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runConfirmationConsumer() error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("confirmation consumer did not stop after cancellation")
	}
	if consumer.calls != 2 {
		t.Fatalf("Run() calls = %d, want 2", consumer.calls)
	}
}

type fakeConfirmationRunner struct {
	calls   int
	started chan struct{}
}

func (r *fakeConfirmationRunner) Run(ctx context.Context) error {
	r.calls++
	r.started <- struct{}{}
	if r.calls == 1 {
		return errors.New("kafka unavailable")
	}
	<-ctx.Done()
	return ctx.Err()
}
