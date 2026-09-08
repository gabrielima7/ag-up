import re

with open("internal/ui/ui.go", "r") as f:
    content = f.read()

old_struct = """type interactiveReader struct {
	reader *bufio.Reader
	reqCh  chan chan string
}

func newInteractiveReader(ctx context.Context, r *bufio.Reader) *interactiveReader {
	ir := &interactiveReader{
		reader: r,
		reqCh:  make(chan chan string),
	}"""

new_struct = """type interactiveReader struct {
	reader *bufio.Reader
	reqCh  chan chan string
	cancel context.CancelFunc
}

func newInteractiveReader(ctx context.Context, r *bufio.Reader) *interactiveReader {
	ctx, cancel := context.WithCancel(ctx)
	ir := &interactiveReader{
		reader: r,
		reqCh:  make(chan chan string),
		cancel: cancel,
	}"""

content = content.replace(old_struct, new_struct)

old_close = """// Close gracefully terminates the background goroutine.
func (ir *interactiveReader) Close() {
	// Let the context handle cancellation. No need to close reqCh to avoid panics on concurrent Close/readLine.
}"""

new_close = """// Close gracefully terminates the background goroutine.
func (ir *interactiveReader) Close() {
	ir.cancel()
}"""

content = content.replace(old_close, new_close)

with open("internal/ui/ui.go", "w") as f:
    f.write(content)
