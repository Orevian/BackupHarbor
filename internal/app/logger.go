package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"backupharbor/internal/config"
	"backupharbor/internal/secret"
)

var levelRank = map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}

// maxLogSize is the size at which the log file is rotated once.
const maxLogSize = 5 << 20

// Logger writes redacted log lines to a rotating file and to optional sinks.
// Log lines never contain secrets: every message passes through secret.Redact.
type Logger struct {
	mu    sync.Mutex
	level int
	file  io.WriteCloser
	sinks []func(level, line string)
}

// LogPath is the log file location.
func LogPath() string { return filepath.Join(config.DataDir(), "logs", "backupharbor.log") }

// NewLogger opens the log file (best effort: logging problems never stop a run).
func NewLogger(level string) *Logger {
	l := &Logger{level: levelRank[level]}
	p := LogPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		if fi, err := os.Stat(p); err == nil && fi.Size() > maxLogSize {
			_ = os.Remove(p + ".1")
			_ = os.Rename(p, p+".1")
		}
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			l.file = f
		}
	}
	return l
}

// AddSink registers a callback receiving every line at or above the log level.
func (l *Logger) AddSink(fn func(level, line string)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sinks = append(l.sinks, fn)
}

// Logf logs a message.
func (l *Logger) Logf(level, format string, args ...any) {
	if l == nil || levelRank[level] < l.level {
		return
	}
	msg := secret.Redact(fmt.Sprintf(format, args...))
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		fmt.Fprintf(l.file, "%s %-5s %s\n", time.Now().Format("2006-01-02T15:04:05.000Z07:00"), level, msg)
	}
	for _, s := range l.sinks {
		s(level, msg)
	}
}

// Close releases the log file.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}
