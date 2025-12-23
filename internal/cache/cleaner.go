package cache

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

import (
	config "boltcache/config"
	logger "boltcache/logger"
)

// StartDataCleaner starts a background goroutine that periodically
// cleans up old data files from the ./data directory.
//
// This function is meant to be called once when the application
// or cache instance starts.
//
// It runs forever in the background and does NOT block the main thread.
func (c *BoltCache) StartDataCleaner() {
	go func() {
		// Create a ticker that triggers every 1 hour
		// ticker := time.NewTicker(5 * time.Second) // FOR_DEV_TEST
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			c.cleanOldDataFiles(&c.Config.Persistence)
		}
	}()
}

// cleanOldDataFiles removes old backup files based on persistence configuration.
//
// The main persistence file is NEVER deleted.
// Cleanup is triggered only when the number of backup files exceeds
// CleanupWhenExceeds. When triggered, the oldest backups are removed
// until only BackupCount newest backups remain.
//
// This batch-based cleanup strategy prevents excessive disk IO
// and log spam caused by frequent backup creation.
func (c *BoltCache) cleanOldDataFiles(persistConf *config.PersistenceConfig) {
	if persistConf == nil ||
		persistConf.BackupCount <= 0 ||
		persistConf.CleanupWhenExceeds <= 0 {
		return
	}

	mainFile := filepath.Base(persistConf.File)
	dataDir := filepath.Dir(persistConf.File)

	files, err := os.ReadDir(dataDir)
	if err != nil {
		logger.Log("Cleaner read error: %v", err)
		return
	}

	type backupFile struct {
		name    string
		modTime time.Time
	}

	var backups []backupFile

	for _, f := range files {
		// main file never delete
		if f.Name() == mainFile {
			continue
		}
		if strings.HasPrefix(f.Name(), mainFile+".backup.") {
			info, err := f.Info()
			if err != nil {
				continue
			}

			backups = append(backups, backupFile{
				name:    f.Name(),
				modTime: info.ModTime(),
			})
		}
	}

	if len(backups) <= persistConf.BackupCount {
		return
	}

	// Do not run cleanup on every backup creation.
	// Cleanup is triggered only when the number of backups
	// exceeds CleanupWhenExceeds to avoid excessive IO and log spam.
	if len(backups) < persistConf.CleanupWhenExceeds {
		return
	}

	// sort old -> new
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.Before(backups[j].modTime)
	})

	toDelete := len(backups) - persistConf.BackupCount
	deletedCount := 0

	for i := 0; i < toDelete; i++ {
		path := filepath.Join(dataDir, backups[i].name)
		if err := os.Remove(path); err == nil {
			deletedCount++
		}
	}

	if deletedCount > 0 {
		logger.Log(
			"Backup cleanup completed. Reduced backups from %d to %d.",
			len(backups),
			persistConf.BackupCount,
		)

	}
}
