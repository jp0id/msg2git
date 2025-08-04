package github

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/msg2git/msg2git/internal/logger"
)

// FileLockManager manages per-file mutex locks for GitHub operations
type FileLockManager struct {
	locks   map[string]*fileLock // Key: userID:repoURL:filename
	locksMu sync.RWMutex         // Protects the locks map
}

// fileLock represents a lock for a specific file with reference counting
type fileLock struct {
	mu       sync.RWMutex // The actual file lock
	refCount int          // Number of operations using this lock
	lastUsed time.Time    // For cleanup purposes

	// Expiry tracking for panic recovery
	activeHandles map[string]*lockExpiry // handle_id -> expiry info
	handlesMu     sync.RWMutex           // Protects activeHandles map
}

// lockExpiry tracks when a lock should be automatically released
type lockExpiry struct {
	acquiredAt time.Time
	expiresAt  time.Time
	handleID   string
	exclusive  bool
}

var (
	// Global file lock manager instance
	globalFileLockManager *FileLockManager
	globalLockManagerOnce sync.Once
)

// GetFileLockManager returns the global file lock manager instance
func GetFileLockManager() *FileLockManager {
	globalLockManagerOnce.Do(func() {
		globalFileLockManager = NewFileLockManager()
	})
	return globalFileLockManager
}

// NewFileLockManager creates a new file lock manager
func NewFileLockManager() *FileLockManager {
	flm := &FileLockManager{
		locks: make(map[string]*fileLock),
	}

	// Start cleanup goroutine
	go flm.cleanupWorker()

	return flm
}

// generateLockKey creates a unique key for file locking
// Format: owner/repo:filename (e.g., "msg2git/mynote:issue.md")
func (flm *FileLockManager) generateLockKey(userID int64, repoURL, filename string) string {
	// Extract owner/repo from repository URL
	owner, repo, err := parseRepositoryURL(repoURL)
	if err != nil {
		// Log error but still create a consistent key format
		logger.Error("Failed to parse repository URL for lock key", map[string]interface{}{
			"repo_url": repoURL,
			"error":    err.Error(),
			"user_id":  userID,
		})
		// Use the original repoURL as fallback but ensure consistent format
		return fmt.Sprintf("%s:%s", repoURL, filename)
	}

	return fmt.Sprintf("%s/%s:%s", owner, repo, filename)
}

// AcquireFileLock acquires a lock for a specific file with timeout
func (flm *FileLockManager) AcquireFileLock(ctx context.Context, userID int64, repoURL, filename string, exclusive bool) (*FileLockHandle, error) {
	lockKey := flm.generateLockKey(userID, repoURL, filename)

	// Get or create the file lock
	lock := flm.getOrCreateLock(lockKey)

	// Generate unique handle ID
	handleID := flm.generateHandleID()

	// Calculate expiry time (5 minutes from now as safety net)
	expiresAt := time.Now().Add(5 * time.Minute)

	// Create a handle for this lock acquisition
	handle := &FileLockHandle{
		lockKey:   lockKey,
		lock:      lock,
		exclusive: exclusive,
		flm:       flm,
		handleID:  handleID,
		expiresAt: expiresAt,
	}

	// Try to acquire the lock with timeout
	lockAcquired := make(chan struct{})
	var lockErr error

	go func() {
		defer func() {
			if r := recover(); r != nil {
				lockErr = fmt.Errorf("panic during lock acquisition: %v", r)
			}
			close(lockAcquired)
		}()

		if exclusive {
			lock.mu.Lock()
		} else {
			lock.mu.RLock()
		}

		// Register this handle for expiry tracking
		flm.registerHandle(lock, handleID, expiresAt, exclusive)

		logger.Debug("File lock acquired", map[string]interface{}{
			"lock_key":   lockKey,
			"exclusive":  exclusive,
			"handle_id":  handleID,
			"expires_at": expiresAt,
			"user_id":    userID,
		})
	}()

	// Wait for lock acquisition or timeout
	select {
	case <-lockAcquired:
		if lockErr != nil {
			flm.decrementRefCount(lockKey)
			return nil, lockErr
		}
		return handle, nil
	case <-ctx.Done():
		// Timeout or cancellation
		flm.decrementRefCount(lockKey)
		return nil, fmt.Errorf("failed to acquire file lock for %s: %w", filename, ctx.Err())
	}
}

// FileLockHandle represents a handle to an acquired file lock
type FileLockHandle struct {
	lockKey   string
	lock      *fileLock
	exclusive bool
	flm       *FileLockManager
	released  bool
	releaseMu sync.Mutex
	handleID  string    // Unique identifier for this handle
	expiresAt time.Time // When this lock should auto-expire
}

// Release releases the file lock
func (fh *FileLockHandle) Release() {
	fh.releaseMu.Lock()
	defer fh.releaseMu.Unlock()

	if fh.released {
		return
	}

	// Unregister this handle from expiry tracking
	fh.flm.unregisterHandle(fh.lock, fh.handleID)

	if fh.exclusive {
		fh.lock.mu.Unlock()
	} else {
		fh.lock.mu.RUnlock()
	}

	logger.Debug("File lock released", map[string]interface{}{
		"lock_key":  fh.lockKey,
		"exclusive": fh.exclusive,
		"handle_id": fh.handleID,
	})

	fh.flm.decrementRefCount(fh.lockKey)
	fh.released = true
}

// generateHandleID generates a unique identifier for a lock handle
func (flm *FileLockManager) generateHandleID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// registerHandle registers a handle for expiry tracking
func (flm *FileLockManager) registerHandle(lock *fileLock, handleID string, expiresAt time.Time, exclusive bool) {
	lock.handlesMu.Lock()
	defer lock.handlesMu.Unlock()

	if lock.activeHandles == nil {
		lock.activeHandles = make(map[string]*lockExpiry)
	}

	lock.activeHandles[handleID] = &lockExpiry{
		acquiredAt: time.Now(),
		expiresAt:  expiresAt,
		handleID:   handleID,
		exclusive:  exclusive,
	}
}

// unregisterHandle removes a handle from expiry tracking
func (flm *FileLockManager) unregisterHandle(lock *fileLock, handleID string) {
	lock.handlesMu.Lock()
	defer lock.handlesMu.Unlock()

	if lock.activeHandles != nil {
		delete(lock.activeHandles, handleID)
	}
}

// getOrCreateLock gets an existing lock or creates a new one
func (flm *FileLockManager) getOrCreateLock(lockKey string) *fileLock {
	flm.locksMu.Lock()
	defer flm.locksMu.Unlock()

	lock, exists := flm.locks[lockKey]
	if !exists {
		lock = &fileLock{
			refCount:      0,
			lastUsed:      time.Now(),
			activeHandles: make(map[string]*lockExpiry),
		}
		flm.locks[lockKey] = lock

		logger.Debug("Created new file lock", map[string]interface{}{
			"lock_key": lockKey,
			"format":   "owner/repo:filename",
		})
	}

	lock.refCount++
	lock.lastUsed = time.Now()

	return lock
}

// decrementRefCount decrements the reference count for a lock
func (flm *FileLockManager) decrementRefCount(lockKey string) {
	flm.locksMu.Lock()
	defer flm.locksMu.Unlock()

	if lock, exists := flm.locks[lockKey]; exists {
		lock.refCount--
		lock.lastUsed = time.Now()

		if lock.refCount <= 0 {
			// Mark for cleanup but don't remove immediately
			// The cleanup worker will handle removal
			logger.Debug("File lock marked for cleanup", map[string]interface{}{
				"lock_key": lockKey,
			})
		}
	}
}

// cleanupWorker periodically cleans up unused locks and expired handles
func (flm *FileLockManager) cleanupWorker() {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("File lock cleanup worker panic recovered", map[string]interface{}{
				"panic": r,
			})
			// Restart the cleanup worker
			go flm.cleanupWorker()
		}
	}()

	ticker := time.NewTicker(1 * time.Minute) // Clean up every minute for expired locks
	defer ticker.Stop()

	for range ticker.C {
		flm.cleanupExpiredHandles()
		flm.cleanupUnusedLocks()
	}
}

// cleanupExpiredHandles forcibly releases locks that have expired (panic recovery)
func (flm *FileLockManager) cleanupExpiredHandles() {
	flm.locksMu.RLock()
	defer flm.locksMu.RUnlock()

	now := time.Now()
	expiredCount := 0

	for lockKey, lock := range flm.locks {
		lock.handlesMu.Lock()

		var expiredHandles []string
		for handleID, expiry := range lock.activeHandles {
			if now.After(expiry.expiresAt) {
				expiredHandles = append(expiredHandles, handleID)
			}
		}

		// Force release expired locks
		for _, handleID := range expiredHandles {
			expiry := lock.activeHandles[handleID]

			// Force unlock the mutex (this is the panic recovery mechanism)
			if expiry.exclusive {
				// For exclusive locks, we need to be careful about unlocking
				// Try to unlock, but wrap in recover to handle double-unlock
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Warn("Recovered from panic during force unlock", map[string]interface{}{
								"lock_key":  lockKey,
								"handle_id": handleID,
								"panic":     r,
							})
						}
					}()
					lock.mu.Unlock()
				}()
			} else {
				// For read locks, also handle potential double-unlock
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Warn("Recovered from panic during force RUnlock", map[string]interface{}{
								"lock_key":  lockKey,
								"handle_id": handleID,
								"panic":     r,
							})
						}
					}()
					lock.mu.RUnlock()
				}()
			}

			// Remove from active handles
			delete(lock.activeHandles, handleID)
			expiredCount++

			logger.Warn("Force released expired lock", map[string]interface{}{
				"lock_key":   lockKey,
				"handle_id":  handleID,
				"expired_at": expiry.expiresAt,
				"held_for":   now.Sub(expiry.acquiredAt).String(),
				"exclusive":  expiry.exclusive,
			})
		}

		lock.handlesMu.Unlock()
	}

	if expiredCount > 0 {
		logger.Info("Cleaned up expired file locks", map[string]interface{}{
			"expired_count": expiredCount,
		})
	}
}

// cleanupUnusedLocks removes locks that haven't been used recently
func (flm *FileLockManager) cleanupUnusedLocks() {
	flm.locksMu.Lock()
	defer flm.locksMu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute) // Remove locks unused for 10 minutes
	var toDelete []string

	for lockKey, lock := range flm.locks {
		if lock.refCount <= 0 && lock.lastUsed.Before(cutoff) {
			toDelete = append(toDelete, lockKey)
		}
	}

	for _, lockKey := range toDelete {
		delete(flm.locks, lockKey)
		logger.Debug("Cleaned up unused file lock", map[string]interface{}{
			"lock_key": lockKey,
		})
	}

	if len(toDelete) > 0 {
		logger.Debug("File lock cleanup completed", map[string]interface{}{
			"cleaned_locks":   len(toDelete),
			"remaining_locks": len(flm.locks),
		})
	}
}

// GetStats returns statistics about the file lock manager
func (flm *FileLockManager) GetStats() map[string]interface{} {
	flm.locksMu.RLock()
	defer flm.locksMu.RUnlock()

	totalRefCount := 0
	activeLocks := 0

	for _, lock := range flm.locks {
		totalRefCount += lock.refCount
		if lock.refCount > 0 {
			activeLocks++
		}
	}

	return map[string]interface{}{
		"total_locks":     len(flm.locks),
		"active_locks":    activeLocks,
		"total_ref_count": totalRefCount,
	}
}

// WithFileLock is a helper function that acquires a lock, executes a function, and releases the lock
func (flm *FileLockManager) WithFileLock(ctx context.Context, userID int64, repoURL, filename string, exclusive bool, fn func() error) error {
	// Set a reasonable timeout for lock acquisition
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	handle, err := flm.AcquireFileLock(lockCtx, userID, repoURL, filename, exclusive)
	if err != nil {
		return fmt.Errorf("failed to acquire file lock: %w", err)
	}
	defer handle.Release()

	return fn()
}

