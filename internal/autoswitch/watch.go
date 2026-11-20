package autoswitch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"time"
)

const (
	// DefaultPoll is how often the transcript is checked for new lines.
	DefaultPoll = 300 * time.Millisecond
	// maxPartialLine caps a buffered unterminated line; a longer one (a huge
	// tool result) is skipped up to its newline.
	maxPartialLine = 16 << 20
	readChunk      = 64 << 10
)

// Tail follows one transcript file from the point it was opened.
type Tail struct {
	path   string
	offset int64
	// partial is the bytes of an unterminated last line.
	partial []byte
	// skipping drops bytes until the next newline (over-long line).
	skipping bool
}

// NewTail records where path ends now, so only lines written afterwards are
// seen. A file that does not exist yet is read from its start once it appears.
func NewTail(path string) *Tail {
	t := &Tail{path: path}
	if fi, err := os.Stat(path); err == nil {
		t.offset = fi.Size()
	}
	return t
}

// newTailAt follows path from offset.
func newTailAt(path string, offset int64) *Tail {
	return &Tail{path: path, offset: offset}
}

// Follow polls the file every poll until ctx ends, calling onLimit for each
// new usage-limit line. Read errors are retried on the next poll.
func (t *Tail) Follow(ctx context.Context, poll time.Duration, onLimit func(Limit)) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		for _, line := range t.readLines() {
			if l, ok := DetectLimit(line); ok {
				onLimit(l)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Watch tails path from its current end (waiting for it to exist) until ctx
// ends, calling onLimit for every usage-limit line.
func Watch(ctx context.Context, path string, poll time.Duration, onLimit func(Limit)) {
	NewTail(path).Follow(ctx, poll, onLimit)
}

// readLines returns the complete lines appended since the last call.
func (t *Tail) readLines() [][]byte {
	data, err := t.readNew()
	if err != nil || len(data) == 0 {
		return nil
	}
	return t.split(data)
}

// readNew reads the bytes after offset; a shrunk file is read from the start.
func (t *Tail) readNew() ([]byte, error) {
	f, err := os.Open(t.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() < t.offset {
		t.offset, t.partial, t.skipping = 0, nil, false
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	n, err := io.CopyBuffer(&buf, f, make([]byte, readChunk))
	t.offset += n
	if err != nil && !errors.Is(err, io.EOF) {
		return buf.Bytes(), err
	}
	return buf.Bytes(), nil
}

// split joins data onto the pending partial line and returns complete lines.
func (t *Tail) split(data []byte) [][]byte {
	var lines [][]byte
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			t.keepPartial(data)
			return lines
		}
		if !t.skipping {
			lines = append(lines, append(t.partial, data[:i]...))
		}
		t.partial, t.skipping, data = nil, false, data[i+1:]
	}
	return lines
}

func (t *Tail) keepPartial(data []byte) {
	if t.skipping {
		return
	}
	if len(t.partial)+len(data) > maxPartialLine {
		t.partial, t.skipping = nil, true
		return
	}
	t.partial = append(t.partial, data...)
}
