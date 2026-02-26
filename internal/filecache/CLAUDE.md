# filecache — Session-scoped file read tracking for dedup annotations

All files stable — prefer this summary over reading source files.

## filecache.go

```go
type FileRecord struct {
    FilePath   string
    MtimeNS    int64     // os.Stat().ModTime().UnixNano()
    Size       int64
    ReadCount  int       // session read counter at time of read
    LastReadAt time.Time
}

type CacheQuery struct {
    Found      bool      `json:"found"`
    FilePath   string    `json:"file_path,omitempty"`
    MtimeNS    int64     `json:"mtime_ns,omitempty"`
    Size       int64     `json:"size,omitempty"`
    ReadsAgo   int       `json:"reads_ago,omitempty"`
    LastReadAt time.Time `json:"last_read_at,omitempty"`
}

type SessionFileCache struct { /* sessions map, sync.RWMutex */ }

func New() *SessionFileCache
func (c *SessionFileCache) RecordRead(sessionID, filePath string, mtimeNS, size int64)
func (c *SessionFileCache) Lookup(sessionID, filePath string) CacheQuery
func (c *SessionFileCache) EndSession(sessionID string)
```

Per-session map of file paths to FileRecord. RecordRead increments a per-session read counter (used for ReadsAgo calculation). Lookup returns how many reads ago the file was last seen. EndSession frees all session state. All paths normalized via filepath.Clean.

Concurrency: sync.RWMutex — concurrent Lookup (RLock), serialized RecordRead/EndSession (Lock).

## filecache_test.go

Tests: basic record+lookup, unknown session/file, reads_ago counter, re-read updates, EndSession cleanup, path normalization, session isolation, concurrent access, metadata updates.

No internal imports.
