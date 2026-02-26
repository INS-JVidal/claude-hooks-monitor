package filecache

import (
	"path/filepath"
	"sync"
	"time"
)

// FileRecord stores metadata from the last Read of a file within a session.
type FileRecord struct {
	FilePath   string    `json:"file_path"`
	MtimeNS   int64     `json:"mtime_ns"`
	Size       int64     `json:"size"`
	ReadCount  int       `json:"read_count"`
	LastReadAt time.Time `json:"last_read_at"`
}

// CacheQuery is the JSON response for a file cache lookup.
type CacheQuery struct {
	Found      bool      `json:"found"`
	FilePath   string    `json:"file_path,omitempty"`
	MtimeNS    int64     `json:"mtime_ns,omitempty"`
	Size       int64     `json:"size,omitempty"`
	ReadsAgo   int       `json:"reads_ago,omitempty"`
	LastReadAt time.Time `json:"last_read_at,omitempty"`
}

// sessionData holds per-session state: file records and a monotonic read counter.
type sessionData struct {
	files     map[string]FileRecord // keyed by cleaned file path
	readCount int                   // incremented on each RecordRead
}

// SessionFileCache tracks file read metadata per session.
// Thread-safe for concurrent access from HTTP handlers.
type SessionFileCache struct {
	mu       sync.RWMutex
	sessions map[string]*sessionData
}

// New creates an empty SessionFileCache.
func New() *SessionFileCache {
	return &SessionFileCache{
		sessions: make(map[string]*sessionData),
	}
}

// RecordRead stores or updates a file's metadata for the given session.
// The file path is cleaned (filepath.Clean) for consistent lookup.
func (c *SessionFileCache) RecordRead(sessionID, filePath string, mtimeNS, size int64) {
	cleanPath := filepath.Clean(filePath)

	c.mu.Lock()
	defer c.mu.Unlock()

	sd, ok := c.sessions[sessionID]
	if !ok {
		sd = &sessionData{
			files: make(map[string]FileRecord),
		}
		c.sessions[sessionID] = sd
	}

	sd.readCount++
	sd.files[cleanPath] = FileRecord{
		FilePath:   cleanPath,
		MtimeNS:    mtimeNS,
		Size:       size,
		ReadCount:  sd.readCount,
		LastReadAt: time.Now(),
	}
}

// Lookup returns cached metadata for a file in a session.
// Returns CacheQuery with Found=false if the session or file is unknown.
func (c *SessionFileCache) Lookup(sessionID, filePath string) CacheQuery {
	cleanPath := filepath.Clean(filePath)

	c.mu.RLock()
	defer c.mu.RUnlock()

	sd, ok := c.sessions[sessionID]
	if !ok {
		return CacheQuery{Found: false}
	}

	rec, ok := sd.files[cleanPath]
	if !ok {
		return CacheQuery{Found: false}
	}

	return CacheQuery{
		Found:      true,
		FilePath:   rec.FilePath,
		MtimeNS:    rec.MtimeNS,
		Size:       rec.Size,
		ReadsAgo:   sd.readCount - rec.ReadCount,
		LastReadAt: rec.LastReadAt,
	}
}

// EndSession removes all cached data for a session, freeing memory.
func (c *SessionFileCache) EndSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, sessionID)
}
