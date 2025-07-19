package github

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	gitconfig "github.com/msg2git/msg2git/internal/config"
)

func TestFileLockManager(t *testing.T) {
	flm := NewFileLockManager()
	
	userID := int64(123)
	repoURL := "https://github.com/user/repo"
	filename := "test.md"
	
	t.Run("Basic lock acquisition and release", func(t *testing.T) {
		ctx := context.Background()
		
		handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}
		
		if handle == nil {
			t.Fatal("Lock handle should not be nil")
		}
		
		handle.Release()
	})
	
	t.Run("Concurrent lock acquisition", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 2)
		
		// Two goroutines trying to acquire the same lock
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				
				handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
				if err != nil {
					results <- err
					return
				}
				
				// Hold the lock for a brief moment
				time.Sleep(100 * time.Millisecond)
				handle.Release()
				results <- nil
			}(i)
		}
		
		wg.Wait()
		close(results)
		
		// Check results
		successCount := 0
		timeoutCount := 0
		
		for err := range results {
			if err == nil {
				successCount++
			} else if err == context.DeadlineExceeded {
				timeoutCount++
			} else {
				t.Errorf("Unexpected error: %v", err)
			}
		}
		
		// At least one should succeed, and at most one should timeout
		if successCount < 1 {
			t.Error("At least one goroutine should succeed")
		}
		if successCount+timeoutCount != 2 {
			t.Error("All goroutines should either succeed or timeout")
		}
	})
	
	t.Run("Different files don't block each other", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 2)
		
		// Two goroutines acquiring locks on different files
		filenames := []string{"file1.md", "file2.md"}
		
		for i, fname := range filenames {
			wg.Add(1)
			go func(id int, filename string) {
				defer wg.Done()
				
				ctx := context.Background()
				handle, err := flm.AcquireFileLock(ctx, userID, repoURL, filename, true)
				if err != nil {
					results <- err
					return
				}
				
				// Hold the lock briefly
				time.Sleep(50 * time.Millisecond)
				handle.Release()
				results <- nil
			}(i, fname)
		}
		
		wg.Wait()
		close(results)
		
		// Both should succeed since they're different files
		for err := range results {
			if err != nil {
				t.Errorf("Both goroutines should succeed, got error: %v", err)
			}
		}
	})
	
	t.Run("WithFileLock helper function", func(t *testing.T) {
		executed := false
		
		ctx := context.Background()
		err := flm.WithFileLock(ctx, userID, repoURL, filename, true, func() error {
			executed = true
			return nil
		})
		
		if err != nil {
			t.Fatalf("WithFileLock failed: %v", err)
		}
		
		if !executed {
			t.Error("Function should have been executed")
		}
	})
	
	t.Run("Lock timeout", func(t *testing.T) {
		// First, acquire a lock and hold it
		ctx1 := context.Background()
		handle1, err := flm.AcquireFileLock(ctx1, userID, repoURL, filename, true)
		if err != nil {
			t.Fatalf("Failed to acquire first lock: %v", err)
		}
		
		// Try to acquire the same lock with a very short timeout
		ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		
		_, err = flm.AcquireFileLock(ctx2, userID, repoURL, filename, true)
		if err == nil {
			t.Error("Second lock acquisition should have failed due to timeout")
		}
		
		// Release the first lock
		handle1.Release()
	})
}

func TestFileLockManagerStats(t *testing.T) {
	flm := NewFileLockManager()
	
	stats := flm.GetStats()
	if stats["total_locks"].(int) != 0 {
		t.Error("Initial total_locks should be 0")
	}
	
	// Acquire a lock
	ctx := context.Background()
	handle, err := flm.AcquireFileLock(ctx, 123, "repo", "file.md", true)
	if err != nil {
		t.Fatalf("Failed to acquire lock: %v", err)
	}
	
	stats = flm.GetStats()
	if stats["total_locks"].(int) != 1 {
		t.Error("total_locks should be 1 after acquiring a lock")
	}
	if stats["active_locks"].(int) != 1 {
		t.Error("active_locks should be 1")
	}
	
	handle.Release()
	
	// Stats might still show the lock but with 0 ref count
	stats = flm.GetStats()
	if stats["active_locks"].(int) != 0 {
		t.Error("active_locks should be 0 after releasing")
	}
}

func TestFileLockKeyGeneration(t *testing.T) {
	flm := NewFileLockManager()
	
	key1 := flm.generateLockKey(123, "repo1", "file1.md")
	key2 := flm.generateLockKey(123, "repo1", "file2.md")
	key3 := flm.generateLockKey(123, "repo2", "file1.md")
	key4 := flm.generateLockKey(456, "repo1", "file1.md")
	
	// All keys should be different
	keys := []string{key1, key2, key3, key4}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] == keys[j] {
				t.Errorf("Keys should be unique: %s == %s", keys[i], keys[j])
			}
		}
	}
	
	// Same parameters should generate same key
	key5 := flm.generateLockKey(123, "repo1", "file1.md")
	if key1 != key5 {
		t.Errorf("Same parameters should generate same key: %s != %s", key1, key5)
	}
}

func TestUserIDParsing(t *testing.T) {
	// Test API provider user ID parsing
	provider := &APIBasedProvider{
		config: &ProviderConfig{
			UserID: "user_889935582",
		},
	}
	
	userID, err := provider.getUserIDForLocking()
	if err != nil {
		t.Fatalf("Failed to parse user ID: %v", err)
	}
	
	expectedID := int64(889935582)
	if userID != expectedID {
		t.Errorf("Expected user ID %d, got %d", expectedID, userID)
	}
	
	// Test with pure numeric ID
	provider.config.UserID = "123456"
	userID, err = provider.getUserIDForLocking()
	if err != nil {
		t.Fatalf("Failed to parse numeric user ID: %v", err)
	}
	
	expectedID = int64(123456)
	if userID != expectedID {
		t.Errorf("Expected user ID %d, got %d", expectedID, userID)
	}
	
	// Test with invalid format (should use hash fallback)
	provider.config.UserID = "invalid_user_format_123abc"
	userID, err = provider.getUserIDForLocking()
	if err != nil {
		t.Fatalf("Should not error with invalid format: %v", err)
	}
	
	if userID <= 0 {
		t.Error("Hash fallback should produce positive user ID")
	}
	
	// Test with empty user ID
	provider.config.UserID = ""
	userID, err = provider.getUserIDForLocking()
	if err != nil {
		t.Fatalf("Should not error with empty user ID: %v", err)
	}
	
	if userID != 0 {
		t.Errorf("Empty user ID should return 0, got %d", userID)
	}
}

func TestManagerUserIDParsing(t *testing.T) {
	// Test Manager user ID parsing
	manager := &Manager{
		userID: "user_889935582",
	}
	
	userID := manager.getUserIDForLocking()
	expectedID := int64(889935582)
	if userID != expectedID {
		t.Errorf("Expected user ID %d, got %d", expectedID, userID)
	}
	
	// Test with pure numeric ID
	manager.userID = "123456"
	userID = manager.getUserIDForLocking()
	expectedID = int64(123456)
	if userID != expectedID {
		t.Errorf("Expected user ID %d, got %d", expectedID, userID)
	}
	
	// Test with invalid format (should use hash fallback)
	manager.userID = "invalid_user_format_123abc"
	userID = manager.getUserIDForLocking()
	if userID <= 0 {
		t.Error("Hash fallback should produce positive user ID")
	}
	
	// Test with empty user ID but with repo fallback
	manager.userID = ""
	manager.cfg = &gitconfig.Config{
		GitHubRepo: "https://github.com/user/repo",
	}
	userID = manager.getUserIDForLocking()
	if userID <= 0 {
		t.Error("Repo hash fallback should produce positive user ID")
	}
}

func TestMultipleFileLocking(t *testing.T) {
	flm := NewFileLockManager()
	userID := int64(123)
	repoURL := "https://github.com/user/repo"
	
	// Test concurrent access to multiple files
	t.Run("Multiple files don't block each other", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 2)
		
		files := []string{"issue.md", "issue_archived.md"}
		
		for i, filename := range files {
			wg.Add(1)
			go func(id int, fname string) {
				defer wg.Done()
				
				ctx := context.Background()
				handle, err := flm.AcquireFileLock(ctx, userID, repoURL, fname, true)
				if err != nil {
					results <- err
					return
				}
				
				// Hold the lock briefly
				time.Sleep(50 * time.Millisecond)
				handle.Release()
				results <- nil
			}(i, filename)
		}
		
		wg.Wait()
		close(results)
		
		// Both should succeed since they're different files
		for err := range results {
			if err != nil {
				t.Errorf("Both goroutines should succeed with different files, got error: %v", err)
			}
		}
	})
	
	t.Run("Same file blocks concurrent access", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan error, 2)
		
		// Two goroutines trying to lock the same file
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
				
				handle, err := flm.AcquireFileLock(ctx, userID, repoURL, "issue.md", true)
				if err != nil {
					results <- err
					return
				}
				
				// Hold the lock for longer than the timeout
				time.Sleep(150 * time.Millisecond)
				handle.Release()
				results <- nil
			}(i)
		}
		
		wg.Wait()
		close(results)
		
		// Check results - one should succeed, one should timeout
		successCount := 0
		timeoutCount := 0
		
		for err := range results {
			if err == nil {
				successCount++
			} else if err == context.DeadlineExceeded || strings.Contains(err.Error(), "context deadline exceeded") {
				timeoutCount++
			} else {
				t.Logf("Got error: %v", err)
			}
		}
		
		// At least one should succeed, and at least one should timeout
		if successCount < 1 {
			t.Error("At least one goroutine should succeed")
		}
		if timeoutCount < 1 {
			t.Error("At least one goroutine should timeout due to lock contention")
		}
	})
}

func TestIssueSyncLockingScenario(t *testing.T) {
	flm := NewFileLockManager()
	userID := int64(123)
	repoURL := "https://github.com/user/repo"
	
	// Simulate the /sync command scenario where both issue.md and issue_archived.md are locked
	t.Run("Sync command locks both issue files", func(t *testing.T) {
		ctx := context.Background()
		
		// Acquire locks for both files (as the sync command would)
		handle1, err := flm.AcquireFileLock(ctx, userID, repoURL, "issue.md", true)
		if err != nil {
			t.Fatalf("Failed to acquire lock for issue.md: %v", err)
		}
		defer handle1.Release()
		
		handle2, err := flm.AcquireFileLock(ctx, userID, repoURL, "issue_archived.md", true)
		if err != nil {
			t.Fatalf("Failed to acquire lock for issue_archived.md: %v", err)
		}
		defer handle2.Release()
		
		// Try to acquire a lock for issue.md from another goroutine (should block/timeout)
		ctx2, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		
		_, err = flm.AcquireFileLock(ctx2, userID, repoURL, "issue.md", true)
		if err == nil {
			t.Error("Should not be able to acquire lock for issue.md when already locked")
		}
		
		// But should be able to acquire lock for a different file
		handle3, err := flm.AcquireFileLock(ctx, userID, repoURL, "inbox.md", true)
		if err != nil {
			t.Errorf("Should be able to acquire lock for different file: %v", err)
		}
		if handle3 != nil {
			handle3.Release()
		}
	})
}