package autoswitch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	transcriptExt = ".jsonl"
	// DefaultDirScan is how often DirTail looks for a newer transcript.
	DefaultDirScan = time.Second
)

// DirTail follows the conversation of one claude run inside its project
// directory. It starts on <id>.jsonl and moves to the newest top-level
// transcript written since launch when one is newer (/clear or /resume
// inside claude start or pick another file), so limits are seen in whichever
// conversation claude is in. Subagent transcripts (<id>/subagents/) and files
// not named like a session id are ignored.
type DirTail struct {
	dir       string
	since     time.Time
	scanEvery time.Duration
	lastScan  time.Time
	// atLaunch is each transcript's size at launch; a file is read from
	// there (0 for a file created later).
	atLaunch map[string]int64
	tails    map[string]*Tail
	cur      string
	tail     *Tail
}

// NewDirTail snapshots dir and starts following session id from the current
// end of its transcript (or its start once it appears).
func NewDirTail(dir, id string, since time.Time) *DirTail {
	d := &DirTail{
		dir:       dir,
		since:     since.Truncate(time.Millisecond),
		scanEvery: DefaultDirScan,
		atLaunch:  transcriptSizes(dir),
		tails:     map[string]*Tail{},
	}
	d.switchTo(id)
	return d
}

// Follow polls every poll until ctx ends, calling onLimit for each new
// usage-limit line (tagged with its session id). Lines stamped before
// launch (history copied into a new file) are skipped.
func (d *DirTail) Follow(ctx context.Context, poll time.Duration, onLimit func(Limit)) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		d.emit(onLimit)
		if d.refresh() {
			d.emit(onLimit)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Session is the id of the transcript being followed.
func (d *DirTail) Session() string { return d.cur }

func (d *DirTail) emit(onLimit func(Limit)) {
	for _, line := range d.tail.readLines() {
		l, ok := DetectLimit(line)
		if !ok || (!l.At.IsZero() && l.At.Before(d.since)) {
			continue
		}
		l.Session = d.cur
		onLimit(l)
	}
}

func (d *DirTail) switchTo(id string) {
	t, ok := d.tails[id]
	if !ok {
		t = newTailAt(filepath.Join(d.dir, id+transcriptExt), d.atLaunch[id])
		d.tails[id] = t
	}
	d.cur, d.tail = id, t
}

// refresh switches to the newest transcript written since launch when it is
// newer than the current one; true when it switched.
func (d *DirTail) refresh() bool {
	now := time.Now()
	if now.Sub(d.lastScan) < d.scanEvery {
		return false
	}
	d.lastScan = now
	id, mtime, ok := newestSince(d.dir, d.since)
	if !ok || id == d.cur {
		return false
	}
	if fi, err := os.Stat(d.tail.path); err == nil && !mtime.After(fi.ModTime()) {
		return false
	}
	d.switchTo(id)
	return true
}

// sessionFiles calls fn for every top-level session transcript in dir.
func sessionFiles(dir string, fn func(id string, fi os.FileInfo)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // not created yet: nothing to follow but the start file
	}
	for _, e := range entries {
		id, isTranscript := strings.CutSuffix(e.Name(), transcriptExt)
		if e.IsDir() || !isTranscript || !sessionIDRE.MatchString(id) {
			continue
		}
		if fi, err := e.Info(); err == nil {
			fn(id, fi)
		}
	}
}

func transcriptSizes(dir string) map[string]int64 {
	out := map[string]int64{}
	sessionFiles(dir, func(id string, fi os.FileInfo) { out[id] = fi.Size() })
	return out
}

// newestSince is the transcript with the latest mtime not before since.
func newestSince(dir string, since time.Time) (string, time.Time, bool) {
	var best string
	var bestAt time.Time
	sessionFiles(dir, func(id string, fi os.FileInfo) {
		at := fi.ModTime()
		if !at.Before(since) && (best == "" || at.After(bestAt)) {
			best, bestAt = id, at
		}
	})
	return best, bestAt, best != ""
}
