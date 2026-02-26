package filecache

import (
	"fmt"
	"sync"
	"testing"
)

func TestRecordAndLookup_Basic(t *testing.T) {
	t.Parallel()
	c := New()
	c.RecordRead("sess1", "/home/user/handler.go", 1000000, 4096)

	q := c.Lookup("sess1", "/home/user/handler.go")
	if !q.Found {
		t.Fatal("expected Found=true")
	}
	if q.MtimeNS != 1000000 {
		t.Errorf("MtimeNS = %d, want 1000000", q.MtimeNS)
	}
	if q.Size != 4096 {
		t.Errorf("Size = %d, want 4096", q.Size)
	}
	if q.ReadsAgo != 0 {
		t.Errorf("ReadsAgo = %d, want 0 (just recorded)", q.ReadsAgo)
	}
}

func TestLookup_UnknownSession(t *testing.T) {
	t.Parallel()
	c := New()
	q := c.Lookup("nonexistent", "/some/file.go")
	if q.Found {
		t.Error("expected Found=false for unknown session")
	}
}

func TestLookup_UnknownFile(t *testing.T) {
	t.Parallel()
	c := New()
	c.RecordRead("sess1", "/home/user/known.go", 100, 200)

	q := c.Lookup("sess1", "/home/user/unknown.go")
	if q.Found {
		t.Error("expected Found=false for unknown file")
	}
}

func TestReadsAgo_Counter(t *testing.T) {
	t.Parallel()
	c := New()

	// Read file A, then B, then C
	c.RecordRead("sess1", "/a.go", 100, 10)
	c.RecordRead("sess1", "/b.go", 200, 20)
	c.RecordRead("sess1", "/c.go", 300, 30)

	// A was read 2 reads ago (reads: A@1, B@2, C@3 → current=3, A.ReadCount=1, diff=2)
	q := c.Lookup("sess1", "/a.go")
	if q.ReadsAgo != 2 {
		t.Errorf("ReadsAgo for /a.go = %d, want 2", q.ReadsAgo)
	}

	// B was read 1 read ago
	q = c.Lookup("sess1", "/b.go")
	if q.ReadsAgo != 1 {
		t.Errorf("ReadsAgo for /b.go = %d, want 1", q.ReadsAgo)
	}

	// C was just read
	q = c.Lookup("sess1", "/c.go")
	if q.ReadsAgo != 0 {
		t.Errorf("ReadsAgo for /c.go = %d, want 0", q.ReadsAgo)
	}
}

func TestReadsAgo_RereadUpdates(t *testing.T) {
	t.Parallel()
	c := New()

	c.RecordRead("sess1", "/a.go", 100, 10)
	c.RecordRead("sess1", "/b.go", 200, 20)
	// Re-read A — should reset its counter
	c.RecordRead("sess1", "/a.go", 100, 10)

	q := c.Lookup("sess1", "/a.go")
	if q.ReadsAgo != 0 {
		t.Errorf("ReadsAgo for /a.go after re-read = %d, want 0", q.ReadsAgo)
	}

	q = c.Lookup("sess1", "/b.go")
	if q.ReadsAgo != 1 {
		t.Errorf("ReadsAgo for /b.go = %d, want 1", q.ReadsAgo)
	}
}

func TestEndSession(t *testing.T) {
	t.Parallel()
	c := New()
	c.RecordRead("sess1", "/a.go", 100, 10)
	c.RecordRead("sess2", "/b.go", 200, 20)

	c.EndSession("sess1")

	q := c.Lookup("sess1", "/a.go")
	if q.Found {
		t.Error("expected Found=false after EndSession")
	}

	// sess2 should be unaffected
	q = c.Lookup("sess2", "/b.go")
	if !q.Found {
		t.Error("expected sess2 to survive EndSession of sess1")
	}
}

func TestEndSession_Idempotent(t *testing.T) {
	t.Parallel()
	c := New()
	// Should not panic
	c.EndSession("nonexistent")
	c.EndSession("nonexistent")
}

func TestPathNormalization(t *testing.T) {
	t.Parallel()
	c := New()

	// Record with trailing slash, lookup without
	c.RecordRead("sess1", "/home/user/dir/./file.go", 100, 10)
	q := c.Lookup("sess1", "/home/user/dir/file.go")
	if !q.Found {
		t.Error("expected path normalization to match ./file.go with file.go")
	}

	// Double slashes
	c.RecordRead("sess1", "/home//user//other.go", 200, 20)
	q = c.Lookup("sess1", "/home/user/other.go")
	if !q.Found {
		t.Error("expected path normalization to match double-slash paths")
	}
}

func TestMultipleSessions_Isolated(t *testing.T) {
	t.Parallel()
	c := New()

	c.RecordRead("sess1", "/file.go", 100, 10)
	c.RecordRead("sess2", "/file.go", 200, 20)

	q1 := c.Lookup("sess1", "/file.go")
	q2 := c.Lookup("sess2", "/file.go")

	if q1.MtimeNS != 100 || q2.MtimeNS != 200 {
		t.Errorf("sessions should be isolated: sess1.mtime=%d, sess2.mtime=%d", q1.MtimeNS, q2.MtimeNS)
	}
}

func TestConcurrent_RecordAndLookup(t *testing.T) {
	t.Parallel()
	c := New()
	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			sess := fmt.Sprintf("sess%d", g%5)
			for i := 0; i < opsPerGoroutine; i++ {
				path := fmt.Sprintf("/file%d.go", i%10)
				c.RecordRead(sess, path, int64(i), int64(i*100))
				c.Lookup(sess, path)
			}
		}(g)
	}
	wg.Wait()

	// Verify no panic and data is accessible
	for s := 0; s < 5; s++ {
		sess := fmt.Sprintf("sess%d", s)
		q := c.Lookup(sess, "/file0.go")
		if !q.Found {
			t.Errorf("expected to find /file0.go in %s after concurrent writes", sess)
		}
	}
}

func TestConcurrent_EndSession(t *testing.T) {
	t.Parallel()
	c := New()

	// Pre-populate
	for s := 0; s < 10; s++ {
		sess := fmt.Sprintf("sess%d", s)
		for i := 0; i < 20; i++ {
			c.RecordRead(sess, fmt.Sprintf("/file%d.go", i), int64(i), 100)
		}
	}

	var wg sync.WaitGroup
	// Concurrent EndSession + Lookup
	for s := 0; s < 10; s++ {
		wg.Add(2)
		sess := fmt.Sprintf("sess%d", s)
		go func() {
			defer wg.Done()
			c.EndSession(sess)
		}()
		go func() {
			defer wg.Done()
			c.Lookup(sess, "/file0.go")
		}()
	}
	wg.Wait()
}

func TestMetadataUpdate(t *testing.T) {
	t.Parallel()
	c := New()

	// Record initial read
	c.RecordRead("sess1", "/file.go", 100, 1000)
	// File was modified — new mtime and size
	c.RecordRead("sess1", "/file.go", 200, 2000)

	q := c.Lookup("sess1", "/file.go")
	if q.MtimeNS != 200 {
		t.Errorf("MtimeNS = %d, want 200 (should be updated)", q.MtimeNS)
	}
	if q.Size != 2000 {
		t.Errorf("Size = %d, want 2000 (should be updated)", q.Size)
	}
}
