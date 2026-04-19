package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const sessionSecretFile = "config/session_secret"

var (
	secretMu     sync.RWMutex
	secretValue  string
	secretLoaded bool
)

// LoadOrGenerateSessionSecret loads the session secret from file,
// or generates a new one if the file does not exist.
// The secret is used for signing session cookies.
func LoadOrGenerateSessionSecret() string {
	secretMu.Lock()
	defer secretMu.Unlock()

	if secretLoaded {
		return secretValue
	}

	data, err := os.ReadFile(sessionSecretFile)
	if err == nil && len(data) >= 32 {
		secretValue = string(data)
		secretLoaded = true
		slog.Info("session secret loaded from file", "path", sessionSecretFile)
		return secretValue
	}

	secretValue = generateSecret(64)
	if err := os.MkdirAll("config", 0700); err != nil {
		slog.Warn("failed to create config dir for session secret", "error", err)
	} else if err := os.WriteFile(sessionSecretFile, []byte(secretValue), 0600); err != nil {
		slog.Warn("failed to persist session secret", "error", err)
	} else {
		slog.Info("session secret generated and persisted", "path", sessionSecretFile)
	}

	secretLoaded = true
	return secretValue
}

// WatchSessionSecret watches the session secret file for changes and reloads.
// Call this in a goroutine. Cancels when ctx is done.
func WatchSessionSecret(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("session secret file watcher disabled", "error", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add("config/"); err != nil {
		slog.Warn("session secret file watcher failed to add dir", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name == sessionSecretFile && (event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {
				reloadSecretFromFile()
			}
		case <-watcher.Errors:
			// ignore
		}
	}
}

func reloadSecretFromFile() {
	data, err := os.ReadFile(sessionSecretFile)
	if err != nil || len(data) < 32 {
		return
	}
	secretMu.Lock()
	secretValue = string(data)
	secretMu.Unlock()
	slog.Info("session secret reloaded from file")
}

func generateSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// fallback to timestamp-based
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}
