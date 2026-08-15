package ui

import (
	"bufio"
	"context"
	"runtime"
	"testing"
	"time"
)

// slowReader simulates a block on stdin.
type slowReader struct{}

func (s slowReader) Read(p []byte) (n int, err error) {
	time.Sleep(1 * time.Second)
	return 0, nil
}

func TestUIGoroutineLeak(t *testing.T) {
	reader := bufio.NewReader(slowReader{})
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()
	ir := newInteractiveReader(rootCtx, reader)
	defer ir.Close()

	initialGoroutines := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		ir.readLine(ctx)
		cancel()
	}

	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		ir.pressEnterToContinue(ctx)
		cancel()
	}

	time.Sleep(100 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	if finalGoroutines - initialGoroutines > 3 {
		t.Fatalf("Leaked goroutines! Initial: %d, Final: %d", initialGoroutines, finalGoroutines)
	}
}
