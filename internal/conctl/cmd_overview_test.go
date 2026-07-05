package conctl

import (
	"context"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

func TestRunWaitUntilExtendsDefaultTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Millisecond)
	defer cancel()

	rc := &RunContext{
		GF: app.GlobalFlags{
			Format:          "json",
			Output:          "-",
			Timeout:         "30s",
			TimeoutExplicit: false,
		},
		Ctx:    ctx,
		Cancel: cancel,
	}

	calls := 0
	fetch := func() (interface{}, error) {
		calls++
		return map[string]interface{}{"running": true}, nil
	}
	check := func(_ interface{}) bool {
		return calls >= 3
	}

	code := runWaitUntil(rc, "5ms", fetch, check, nil)
	if code != app.ExitSuccess {
		t.Fatalf("expected exit success, got %d", code)
	}
	if calls < 3 {
		t.Fatalf("expected >=3 polls, got %d", calls)
	}
}

func TestRunWaitUntilRespectsExplicitTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Millisecond)
	defer cancel()

	rc := &RunContext{
		GF: app.GlobalFlags{
			Format:          "json",
			Output:          "-",
			Timeout:         "8ms",
			TimeoutExplicit: true,
		},
		Ctx:    ctx,
		Cancel: cancel,
	}

	calls := 0
	fetch := func() (interface{}, error) {
		calls++
		return map[string]interface{}{"running": true}, nil
	}
	check := func(_ interface{}) bool {
		return calls >= 3
	}

	start := time.Now()
	code := runWaitUntil(rc, "5ms", fetch, check, nil)
	if code != app.ExitServer {
		t.Fatalf("expected exit server timeout, got %d", code)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("expected explicit timeout to return quickly, elapsed=%s", time.Since(start))
	}
}
