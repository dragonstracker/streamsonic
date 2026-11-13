package dvr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("dvr: no data in the requested range")

type Entry struct {
	Start int64  `json:"t"`
	Dur   int64  `json:"d"`
	Off   int64  `json:"o"`
	Len   int64  `json:"l"`
	Blob  string `json:"b"`
}

func (e Entry) From() time.Time { return time.UnixMilli(e.Start) }
func (e Entry) End() time.Time  { return time.UnixMilli(e.Start + e.Dur) }

type Lock struct {
	Start   int64  `json:"t"`
	End     int64  `json:"e"`
	Comment string `json:"c,omitempty"`
}

type Range struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type Options struct {
	Root         string
	Expiration   time.Duration
	StorageLimit int64
}

type indexFile struct {
	Stream  string  `json:"stream"`
	Entries []Entry `json:"entries"`
	Locks   []Lock  `json:"locks,omitempty"`
	Updated int64   `json:"updated"`
}

type Archive struct {
	mu      sync.RWMutex
	dir     string
	name    string
	opts    Options
	entries []Entry
	locks   []Lock
	size    int64
	dirty   bool
	saved   time.Time
	lastEnd int64
}

func Open(opts Options, stream string) (*Archive, error) {
	if opts.Root == "" {
		return nil, errors.New("dvr: root is required")
	}
	a := &Archive{dir: filepath.Join(opts.Root, stream), name: stream, opts: opts}
	if err := os.MkdirAll(filepath.Join(a.dir, "blobs"), 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(a.dir, "index.json"))
	if err == nil {
		var idx indexFile
		if err := json.Unmarshal(data, &idx); err == nil {
			a.entries = idx.Entries
			a.locks = idx.Locks
			for _, e := range a.entries {
				a.size += e.Len
				if e.Start+e.Dur > a.lastEnd {
					a.lastEnd = e.Start + e.Dur
				}
			}
		}
	}
	sort.Slice(a.entries, func(i, j int) bool { return a.entries[i].Start < a.entries[j].Start })
	return a, nil
}

func (a *Archive) Name() string { return a.name }
func (a *Archive) Dir() string  { return a.dir }

func (a *Archive) Append(from time.Time, dur time.Duration, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	blob := from.UTC().Format("20060102T15")
	path := filepath.Join(a.dir, "blobs", blob+".ts")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	off, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		f.Close()
		return err
	}
	n, err := f.Write(data)
	f.Close()
	if err != nil {
		return err
	}
	e := Entry{Start: from.UnixMilli(), Dur: dur.Milliseconds(), Off: off, Len: int64(n), Blob: blob}
	a.entries = append(a.entries, e)
	a.size += int64(n)
	if end := e.Start + e.Dur; end > a.lastEnd {
		a.lastEnd = end
	}
	a.dirty = true
	a.enforceLocked()
	return a.saveLocked()
}

func (a *Archive) Entries() []Entry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]Entry, len(a.entries))
	copy(out, a.entries)
	return out
}

func (a *Archive) Locks() []Lock {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]Lock, len(a.locks))
	copy(out, a.locks)
	return out
}

func (a *Archive) Size() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.size
}

func (a *Archive) From() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.entries) == 0 {
		return time.Time{}
	}
	return a.entries[0].From()
}

func (a *Archive) To() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.entries) == 0 {
		return time.Time{}
	}
	last := a.entries[len(a.entries)-1]
	return last.End()
}

func (a *Archive) Ranges() []Range {
	entries := a.Entries()
	var out []Range
	for _, e := range entries {
		if n := len(out); n > 0 && e.Start <= out[n-1].To.UnixMilli()+1000 {
			if e.End().After(out[n-1].To) {
				out[n-1].To = e.End()
			}
			continue
		}
		out = append(out, Range{From: e.From(), To: e.End()})
	}
	return out
}

func (a *Archive) Select(from time.Time, dur time.Duration) []Entry {
	start := from.UnixMilli()
	end := from.Add(dur).UnixMilli()
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []Entry
	for _, e := range a.entries {
		if e.Start+e.Dur <= start || e.Start >= end {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (a *Archive) OpenRange(from time.Time, dur time.Duration) (io.ReadCloser, int64, error) {
	entries := a.Select(from, dur)
	if len(entries) == 0 {
		return nil, 0, ErrNotFound
	}
	var total int64
	for _, e := range entries {
		total += e.Len
	}
	return &rangeReader{archive: a, entries: entries}, total, nil
}

type rangeReader struct {
	archive *Archive
	entries []Entry
	idx     int
	file    *os.File
	remain  int64
}

func (r *rangeReader) Read(p []byte) (int, error) {
	for {
		if r.file == nil {
			if r.idx >= len(r.entries) {
				return 0, io.EOF
			}
			e := r.entries[r.idx]
			f, err := os.Open(filepath.Join(r.archive.dir, "blobs", e.Blob+".ts"))
			if err != nil {
				r.idx++
				continue
			}
			if _, err := f.Seek(e.Off, io.SeekStart); err != nil {
				f.Close()
				r.idx++
				continue
			}
			r.file = f
			r.remain = e.Len
		}
		if r.remain <= 0 {
			r.file.Close()
			r.file = nil
			r.idx++
			continue
		}
		if int64(len(p)) > r.remain {
			p = p[:r.remain]
		}
		n, err := r.file.Read(p)
		r.remain -= int64(n)
		if err == io.EOF {
			r.file.Close()
			r.file = nil
			r.idx++
			continue
		}
		return n, err
	}
}

func (r *rangeReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

func (a *Archive) Lock(from time.Time, dur time.Duration, comment string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.locks = append(a.locks, Lock{Start: from.UnixMilli(), End: from.Add(dur).UnixMilli(), Comment: comment})
	a.dirty = true
	return a.saveLocked()
}

func (a *Archive) Unlock(from time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	start := from.UnixMilli()
	kept := a.locks[:0]
	for _, l := range a.locks {
		if l.Start != start {
			kept = append(kept, l)
		}
	}
	a.locks = kept
	a.dirty = true
	return a.saveLocked()
}

func (a *Archive) Cleanup(now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.enforceLocked()
	a.saveLocked()
}

func (a *Archive) enforceLocked() {
	cutoff := time.Now().Add(-a.opts.Expiration).UnixMilli()
	if a.opts.Expiration <= 0 {
		cutoff = 0
	}
	kept := a.entries[:0]
	var size int64
	for _, e := range a.entries {
		if a.opts.Expiration > 0 && e.Start+e.Dur < cutoff && !a.lockedLocked(e.Start, e.Start+e.Dur) {
			continue
		}
		kept = append(kept, e)
		size += e.Len
	}
	a.entries = kept
	if a.opts.StorageLimit > 0 && size > a.opts.StorageLimit {
		for len(a.entries) > 0 && size > a.opts.StorageLimit {
			e := a.entries[0]
			if a.lockedLocked(e.Start, e.Start+e.Dur) {
				break
			}
			size -= e.Len
			a.entries = a.entries[1:]
		}
	}
	a.size = size
	a.cleanupBlobsLocked()
}

func (a *Archive) lockedLocked(start, end int64) bool {
	for _, l := range a.locks {
		if start < l.End && end > l.Start {
			return true
		}
	}
	return false
}

func (a *Archive) cleanupBlobsLocked() {
	used := map[string]bool{}
	for _, e := range a.entries {
		used[e.Blob] = true
	}
	dir := filepath.Join(a.dir, "blobs")
	items, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, it := range items {
		name := it.Name()
		if len(name) < 3 {
			continue
		}
		key := name[:len(name)-3]
		if !used[key] {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

func (a *Archive) saveLocked() error {
	if !a.dirty {
		return nil
	}
	idx := indexFile{Stream: a.name, Entries: a.entries, Locks: a.locks, Updated: time.Now().UnixMilli()}
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	tmp := filepath.Join(a.dir, "index.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(a.dir, "index.json")); err != nil {
		return err
	}
	a.dirty = false
	a.saved = time.Now()
	return nil
}

func (a *Archive) Metadata() map[string]any {
	a.mu.RLock()
	from, to := time.Time{}, time.Time{}
	if len(a.entries) > 0 {
		from = a.entries[0].From()
		to = a.entries[len(a.entries)-1].End()
	}
	size := a.size
	n := len(a.entries)
	locks := len(a.locks)
	ranges := make([]Range, 0, 4)
	for _, e := range a.entries {
		if k := len(ranges); k > 0 && e.Start <= ranges[k-1].To.UnixMilli()+1000 {
			if e.End().After(ranges[k-1].To) {
				ranges[k-1].To = e.End()
			}
			continue
		}
		ranges = append(ranges, Range{From: e.From(), To: e.End()})
	}
	exp := a.opts.Expiration
	a.mu.RUnlock()
	return map[string]any{
		"stream":     a.name,
		"from":       from.UTC().Format(time.RFC3339),
		"to":         to.UTC().Format(time.RFC3339),
		"size":       size,
		"entries":    n,
		"ranges":     ranges,
		"locks":      locks,
		"expiration": int(exp.Seconds()),
	}
}

func (a *Archive) Hour(blob string) string {
	return fmt.Sprintf("%s/%s.ts", a.dir, blob)
}
