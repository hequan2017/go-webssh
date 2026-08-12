package core

import (
	"bytes"
	"testing"
)

func TestOutputWriterCopiesData(t *testing.T) {
	output := make(chan []byte, 1)
	done := make(chan struct{})
	writer := outputWriter{ch: output, done: done}
	data := []byte("hello")

	n, err := writer.Write(data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	data[0] = 'x'

	if n != 5 {
		t.Fatalf("Write() = %d, want 5", n)
	}
	if got := <-output; !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("output = %q, want hello", got)
	}
}

func TestOutputWriterStopsWhenClosed(t *testing.T) {
	output := make(chan []byte)
	done := make(chan struct{})
	close(done)

	if _, err := (outputWriter{ch: output, done: done}).Write([]byte("hello")); err == nil {
		t.Fatal("Write() should fail after close")
	}
}
