package filelogger

import (
	"fmt"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// rotate performs log file rotation using the standard rename strategy.
// It shifts existing backups, renames the current file, and opens a new one.
//
// Rotation sequence:
//  1. Flush and close current file
//  2. Shift existing backups: .2 -> .3, .1 -> .2
//  3. Rename current file to .1
//  4. Delete oldest backup if exceeds maxBackups
//  5. Open new log file
//
// Caller must hold l.mu.
func (l *Logger) rotate() error {
	// Flush and close current file
	if l.writer != nil {
		if err := l.writer.Flush(); err != nil {
			log.WithError(err).Warn("Failed to flush before rotation")
		}
	}
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			log.WithError(err).Warn("Failed to close file before rotation")
		}
		l.file = nil
		l.writer = nil
	}

	// Shift existing backups (highest number first to avoid overwriting)
	// e.g., .3 -> delete, .2 -> .3, .1 -> .2
	for i := l.opts.MaxBackups; i >= 1; i-- {
		src := l.backupPath(i)
		dst := l.backupPath(i + 1)

		if i == l.opts.MaxBackups {
			// Delete oldest backup
			if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
				log.WithError(err).WithField("path", src).Warn("Failed to remove old backup")
			}
		} else {
			// Shift backup
			if _, err := os.Stat(src); err == nil {
				if err := os.Rename(src, dst); err != nil {
					log.WithError(err).WithFields(log.Fields{
						"src": src,
						"dst": dst,
					}).Warn("Failed to shift backup")
				}
			}
		}
	}

	// Rename current file to .1
	backup1 := l.backupPath(1)
	if _, err := os.Stat(l.opts.FilePath); err == nil {
		if err := os.Rename(l.opts.FilePath, backup1); err != nil {
			log.WithError(err).WithFields(log.Fields{
				"src": l.opts.FilePath,
				"dst": backup1,
			}).Error("Failed to rename current file")
			// Try to reopen current file anyway
		}
	}

	// Open new log file
	if err := l.openFile(); err != nil {
		return fmt.Errorf("failed to open new log file after rotation: %w", err)
	}

	log.WithFields(log.Fields{
		"session_id": l.sessionID,
		"file_path":  l.opts.FilePath,
	}).Info("Log file rotated")

	return nil
}

// backupPath returns the path for a numbered backup file.
// e.g., /var/log/kubetty/pty-session.jsonl.1
func (l *Logger) backupPath(n int) string {
	return fmt.Sprintf("%s.%d", l.opts.FilePath, n)
}

// CleanupOldBackups removes backup files beyond the configured max.
// This can be called periodically to clean up any stray backups.
func (l *Logger) CleanupOldBackups() {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check for backups beyond maxBackups
	for i := l.opts.MaxBackups + 1; i <= l.opts.MaxBackups+10; i++ {
		path := l.backupPath(i)
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				break // No more backups to clean
			}
			log.WithError(err).WithField("path", path).Warn("Failed to cleanup old backup")
		} else {
			log.WithField("path", path).Debug("Cleaned up old backup")
		}
	}
}

// FileSize returns the current log file size in bytes.
func (l *Logger) FileSize() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.fileSize
}

// FilePath returns the current log file path.
func (l *Logger) FilePath() string {
	return l.opts.FilePath
}

// ListBackups returns a list of existing backup file paths.
func (l *Logger) ListBackups() []string {
	var backups []string
	dir := filepath.Dir(l.opts.FilePath)
	base := filepath.Base(l.opts.FilePath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return backups
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Match pattern: base.N where N is a number
		if len(name) > len(base)+1 && name[:len(base)] == base && name[len(base)] == '.' {
			suffix := name[len(base)+1:]
			if isNumeric(suffix) {
				backups = append(backups, filepath.Join(dir, name))
			}
		}
	}

	return backups
}

// isNumeric checks if a string contains only digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
