// Package ui provides shared terminal UX primitives (spinners, progress, etc.).
package ui

import (
	"fmt"
	"os"
	"time"
)

// spinnerFrames are the braille characters used for the spinner animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// WithSpinner displays an inline braille spinner on stderr while fn executes.
// The message is shown next to the spinner. Returns fn's error.
func WithSpinner(msg string, fn func() error) error {
	stopped := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(stopped)
		i := 0
		for {
			select {
			case <-done:
				fmt.Fprintf(os.Stderr, "\r\033[K") // clear line
				return
			default:
				fmt.Fprintf(os.Stderr, "\r  %s %s", spinnerFrames[i%len(spinnerFrames)], msg)
				time.Sleep(80 * time.Millisecond)
				i++
			}
		}
	}()
	err := fn()
	close(done)
	<-stopped // wait for goroutine to finish clearing the line
	return err
}
