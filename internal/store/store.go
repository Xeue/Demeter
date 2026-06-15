// Package store persists frames and groups as JSON under the data directory,
// with atomic writes and dirty-flag coalescing on a dedicated goroutine (off the
// scan/request path, fixing the legacy blocking, non-atomic writeFileSync).
//
// Legacy compatibility: the on-disk frames.json / groups.json shapes are
// unchanged, so existing files load directly (the model's lenient Value/Num
// unmarshalling absorbs the loose typing). Config migration (config.conf ->
// config.json) lives in the config package; user bootstrap in the auth package.
package store

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/Xeue/Demeter/internal/model"
)

const flushInterval = 2 * time.Second

// Store owns the data directory and coalesced persistence of frames/groups.
type Store struct {
	dataDir string

	mu          sync.Mutex
	frames      model.Frames
	groups      model.Groups
	framesDirty bool
	groupsDirty bool
	flushNow    chan struct{}
}

// New opens (creating if needed) the data directory and loads frames/groups.
func New(dataDir string) (*Store, error) {
	s := &Store{
		dataDir:  dataDir,
		frames:   model.Frames{},
		groups:   model.Groups{},
		flushNow: make(chan struct{}, 1),
	}
	if err := ReadJSON(s.framesPath(), &s.frames); err != nil {
		slog.Warn("store: could not load frames.json, starting empty", "err", err)
		s.frames = model.Frames{}
	}
	if s.frames == nil {
		s.frames = model.Frames{}
	}
	if err := ReadJSON(s.groupsPath(), &s.groups); err != nil {
		slog.Warn("store: could not load groups.json, starting empty", "err", err)
		s.groups = model.Groups{}
	}
	if s.groups == nil {
		s.groups = model.Groups{}
	}
	return s, nil
}

// DataDir returns the data directory path.
func (s *Store) DataDir() string { return s.dataDir }

func (s *Store) framesPath() string { return filepath.Join(s.dataDir, "data", "frames.json") }
func (s *Store) groupsPath() string { return filepath.Join(s.dataDir, "data", "groups.json") }

// Frames returns the loaded frames (used once at startup to seed the manager).
func (s *Store) Frames() model.Frames {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frames
}

// Groups returns the loaded groups (used once at startup to seed the manager).
func (s *Store) Groups() model.Groups {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.groups
}

// SaveFrames records the latest frames to persist. now=true forces an immediate
// flush (deletes, bulk imports); otherwise the flush is coalesced.
func (s *Store) SaveFrames(frames model.Frames, now bool) {
	s.mu.Lock()
	s.frames = frames
	s.framesDirty = true
	s.mu.Unlock()
	if now {
		s.signalFlush()
	}
}

// SaveGroups records the latest groups to persist.
func (s *Store) SaveGroups(groups model.Groups, now bool) {
	s.mu.Lock()
	s.groups = groups
	s.groupsDirty = true
	s.mu.Unlock()
	if now {
		s.signalFlush()
	}
}

func (s *Store) signalFlush() {
	select {
	case s.flushNow <- struct{}{}:
	default:
	}
}

// Run is the persistence goroutine: it flushes dirty files on a ticker and on
// demand, until ctx is cancelled, then flushes a final time.
func (s *Store) Run(ctx context.Context) {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.flush()
			return
		case <-t.C:
			s.flush()
		case <-s.flushNow:
			s.flush()
		}
	}
}

// Close performs a final synchronous flush of any dirty state.
func (s *Store) Close() { s.flush() }

func (s *Store) flush() {
	s.mu.Lock()
	var (
		frames       model.Frames
		groups       model.Groups
		wantF, wantG bool
	)
	if s.framesDirty {
		frames = model.CloneFrames(s.frames)
		s.framesDirty = false
		wantF = true
	}
	if s.groupsDirty {
		groups = model.CloneGroups(s.groups)
		s.groupsDirty = false
		wantG = true
	}
	s.mu.Unlock()

	if wantF {
		if err := WriteJSON(s.framesPath(), frames); err != nil {
			slog.Error("store: write frames.json", "err", err)
		}
	}
	if wantG {
		if err := WriteJSON(s.groupsPath(), groups); err != nil {
			slog.Error("store: write groups.json", "err", err)
		}
	}
}
