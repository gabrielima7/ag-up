package printer

import (
	"fmt"
	"sync"
)

// Mu synchronizes stdout access to prevent interleaved printing during parallel operations.
var Mu sync.Mutex

// Printf is a synchronized wrapper around fmt.Printf.
func Printf(format string, a ...any) {
	Mu.Lock()
	defer Mu.Unlock()
	fmt.Printf(format, a...)
}

// Println is a synchronized wrapper around fmt.Println.
func Println(a ...any) {
	Mu.Lock()
	defer Mu.Unlock()
	fmt.Println(a...)
}

// Print is a synchronized wrapper around fmt.Print.
func Print(a ...any) {
	Mu.Lock()
	defer Mu.Unlock()
	fmt.Print(a...)
}
