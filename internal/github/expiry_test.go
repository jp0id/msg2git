package github

import (
	"context"
	"testing"
	"time"
)

func TestLockExpiry(t *testing.T) {
	flm := NewFileLockManager()
	userID := int64(123)
	repoURL := "https://github.com/user/repo"
	filename := "test.md"
	
	t.Run("Lock expires after 5 minutes", func(t *testing.T) {
		ctx := context.Background()
		
		// Acquire a lock
		handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}
		
		// Verify expiry time is set
		if handle.expiresAt.IsZero() {
			t.Error("Lock should have expiry time set")
		}
		
		expectedExpiry := time.Now().Add(5 * time.Minute)
		if handle.expiresAt.Before(expectedExpiry.Add(-10*time.Second)) || 
		   handle.expiresAt.After(expectedExpiry.Add(10*time.Second)) {
			t.Errorf("Lock expiry time should be around 5 minutes from now, got %v", handle.expiresAt)
		}
		
		// Verify handle is registered
		stats := flm.GetStats()
		if stats["active_locks"].(int) != 1 {
			t.Error("Should have 1 active lock")
		}
		
		// Normal release should unregister the handle
		handle.Release()
		
		// Give some time for cleanup
		time.Sleep(10 * time.Millisecond)
		
		stats = flm.GetStats()
		if stats["active_locks"].(int) != 0 {
			t.Error("Should have 0 active locks after release")
		}
	})
	
	t.Run("Simulated panic recovery", func(t *testing.T) {
		ctx := context.Background()
		
		// Acquire a lock
		handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}
		
		// Manually set expiry to past time to simulate expired lock
		handle.expiresAt = time.Now().Add(-1 * time.Minute)
		
		// Update the expiry in the tracking map
		handle.lock.handlesMu.Lock()
		if expiry, exists := handle.lock.activeHandles[handle.handleID]; exists {
			expiry.expiresAt = handle.expiresAt
		}
		handle.lock.handlesMu.Unlock()
		
		// Trigger cleanup manually
		flm.cleanupExpiredHandles()
		
		// The lock should have been force-released
		// Try to acquire the same lock - should succeed immediately
		ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		
		handle2, err := flm.AcquireFileLock(ctx2, userID, repoURL, filename, true)
		if err != nil {
			t.Errorf("Should be able to acquire lock after expiry cleanup: %v", err)
		}
		
		if handle2 != nil {
			handle2.Release()
		}
	})
}

func TestHandleIDGeneration(t *testing.T) {
	flm := NewFileLockManager()
	
	// Generate multiple handle IDs
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := flm.generateHandleID()
		if len(id) != 16 { // 8 bytes -> 16 hex chars
			t.Errorf("Handle ID should be 16 characters, got %d", len(id))
		}
		
		if ids[id] {
			t.Errorf("Handle ID should be unique, got duplicate: %s", id)
		}
		ids[id] = true
	}
}

func TestExpiredHandleCleanup(t *testing.T) {
	flm := NewFileLockManager()
	userID := int64(123)
	repoURL := "https://github.com/user/repo"
	
	// Create multiple locks with different expiry times
	files := []string{"file1.md", "file2.md", "file3.md"}
	var handles []*FileLockHandle
	
	ctx := context.Background()
	
	for _, filename := range files {
		handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
		if err != nil {
			t.Fatalf("Failed to acquire lock for %s: %v", filename, err)
		}
		handles = append(handles, handle)
	}
	
	// Manually expire some locks
	handles[0].expiresAt = time.Now().Add(-2 * time.Minute)
	handles[1].expiresAt = time.Now().Add(-1 * time.Minute)
	// handles[2] remains valid
	
	// Update expiry times in tracking maps
	for i := 0; i < 2; i++ {
		handles[i].lock.handlesMu.Lock()
		if expiry, exists := handles[i].lock.activeHandles[handles[i].handleID]; exists {
			expiry.expiresAt = handles[i].expiresAt
		}
		handles[i].lock.handlesMu.Unlock()
	}
	
	// Run cleanup
	flm.cleanupExpiredHandles()
	
	// Try to acquire locks for expired files (should succeed)
	for i := 0; i < 2; i++ {
		ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		
		handle, err := flm.AcquireFileLock(ctx2, userID, repoURL, files[i], true)
		if err != nil {
			t.Errorf("Should be able to acquire lock for %s after expiry: %v", files[i], err)
		}
		if handle != nil {
			handle.Release()
		}
		
		cancel()
	}
	
	// The valid lock should still be held
	ctx3, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	
	_, err := flm.AcquireFileLock(ctx3, userID, repoURL, files[2], true)
	if err == nil {
		t.Error("Should not be able to acquire lock for file3.md as it's still held")
	}
	
	// Clean up
	handles[2].Release()
}