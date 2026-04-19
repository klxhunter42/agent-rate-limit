package middleware

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type ConfigChangeCallback func(key, value string)

type ConfigWatcher struct {
	mu       sync.RWMutex
	values   map[string]string
	watcher  *fsnotify.Watcher
	callback ConfigChangeCallback
	file     string
}

// NewConfigWatcher watches a config file (like .env) for changes.
// When a key changes, callback is called with the key and new value.
func NewConfigWatcher(file string, cb ConfigChangeCallback) *ConfigWatcher {
	return &ConfigWatcher{
		values:   loadEnvFile(file),
		callback: cb,
		file:     file,
	}
}

// Start begins watching. Blocks until ctx is cancelled.
func (cw *ConfigWatcher) Start(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("config file watcher disabled", "error", err)
		return
	}
	cw.watcher = watcher
	defer watcher.Close()

	dir := cw.file
	if idx := strings.LastIndex(cw.file, "/"); idx >= 0 {
		dir = cw.file[:idx]
	}
	if err := watcher.Add(dir); err != nil {
		slog.Warn("config watcher failed to add dir", "dir", dir, "error", err)
		return
	}

	slog.Info("config file watcher started", "file", cw.file)

	debounce := make(chan struct{}, 1)
	go func() {
		for range debounce {
			time.Sleep(500 * time.Millisecond)
			cw.checkChanges()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			close(debounce)
			return
		case event, ok := <-watcher.Events:
			if !ok {
				close(debounce)
				return
			}
			if event.Name == cw.file && (event.Op&fsnotify.Write == fsnotify.Write) {
				select {
				case debounce <- struct{}{}:
				default:
				}
			}
		case <-watcher.Errors:
			// ignore
		}
	}
}

func (cw *ConfigWatcher) checkChanges() {
	newValues := loadEnvFile(cw.file)
	cw.mu.Lock()
	defer cw.mu.Unlock()

	for k, nv := range newValues {
		if ov, ok := cw.values[k]; ok && ov != nv {
			slog.Info("config value changed", "key", k)
			if cw.callback != nil {
				cw.callback(k, nv)
			}
		}
	}
	cw.values = newValues
}

func loadEnvFile(path string) map[string]string {
	m := make(map[string]string)
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			m[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return m
}
