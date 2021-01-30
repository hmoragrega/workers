package wrapper

import (
	"context"
	"testing"
)

func TestNoError(t *testing.T) {
	var called bool
	job := func(ctx context.Context) {
		called = true
	}

	got := NoError(job).Do(context.Background())
	if got != nil {
		t.Fatal("unexpected job error", got)
	}
	if !called {
		t.Fatal("wrapped job was not called", got)
	}
}
