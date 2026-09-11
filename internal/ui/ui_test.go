package ui

import (
	"bufio"
	"context"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

// blockingReader simulates blocking on stdin until explicitly unblocked.
type blockingReader struct {
	done chan struct{}
}

func (b *blockingReader) Read(p []byte) (n int, err error) {
	<-b.done
	return 0, io.EOF
}

func TestUIGoroutineLeak(t *testing.T) {
	br := &blockingReader{done: make(chan struct{})}
	defer close(br.done)

	reader := bufio.NewReader(br)
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

	time.Sleep(50 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	if finalGoroutines-initialGoroutines > 3 {
		t.Fatalf("Leaked goroutines! Initial: %d, Final: %d", initialGoroutines, finalGoroutines)
	}
}

func TestUIPostEOF(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("hello\n"))
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	ir := newInteractiveReader(rootCtx, reader)
	defer ir.Close()

	// First read gets "hello"
	line1 := ir.readLine(rootCtx)
	if line1 != "hello" {
		t.Fatalf("expected 'hello', got %q", line1)
	}

	// Subsequent reads should return "" immediately without blocking/hanging
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		line := ir.readLine(ctx)
		cancel()
		if line != "" {
			t.Fatalf("expected empty string on EOF, got %q", line)
		}
	}
}

func TestUICloseCancellation(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("some content\n"))
	rootCtx := context.Background() // explicitly uncancelled root context

	ir := newInteractiveReader(rootCtx, reader)
	// Calling Close cancels the internal context
	ir.Close()

	// Subsequent readLine on another context should immediately exit/return ""
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	line := ir.readLine(ctx)
	if line != "" {
		t.Fatalf("expected empty string after ir.Close(), got %q", line)
	}
}

