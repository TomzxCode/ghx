// Package logging provides a small, CLI-friendly logging facade over
// log/slog. The global level is controlled by the --log-level flag and
// defaults to info. Messages are written to stderr as "level message key=value"
// lines, deliberately without timestamps or source locations so terminal
// output stays readable.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
)

// level is the global threshold, defaulting to info.
var level = func() *slog.LevelVar {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	return lv
}()

var logger = slog.New(newHandler(os.Stderr, level))

// ParseLevel converts a level name into a slog.Level. An empty string is
// treated as info. Names are case-insensitive; "warning" is accepted as an
// alias for "warn".
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q: use debug, info, warn, or error", name)
	}
}

// SetLevel applies the named level to the global logger. It returns an error
// for an unrecognised name.
func SetLevel(name string) error {
	lv, err := ParseLevel(name)
	if err != nil {
		return err
	}
	level.Set(lv)
	return nil
}

// Logger returns the global logger for callers that need the *slog.Logger.
func Logger() *slog.Logger { return logger }

// SetOutput redirects log output to w. It is used by the CLI to route log
// writes through a writer that coordinates with an active progress bar. It
// must be called before any concurrent logging; typically once at startup.
// A nil writer is ignored so the logger can never be pointed at a nil sink.
func SetOutput(w io.Writer) {
	if w == nil {
		return
	}
	logger = slog.New(newHandler(w, level))
}

// Debug logs at debug level.
func Debug(msg string, args ...any) { logger.Debug(msg, args...) }

// Info logs at info level.
func Info(msg string, args ...any) { logger.Info(msg, args...) }

// Warn logs at warn level.
func Warn(msg string, args ...any) { logger.Warn(msg, args...) }

// Error logs at error level.
func Error(msg string, args ...any) { logger.Error(msg, args...) }

// handler is a minimal slog.Handler emitting "level message key=value" lines.
type handler struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Leveler
	attrs  []slog.Attr
	groups []string
}

func newHandler(w io.Writer, l slog.Leveler) slog.Handler {
	return &handler{mu: &sync.Mutex{}, w: w, level: l}
}

func (h *handler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level.Level()
}

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(levelLabel(r.Level))
	b.WriteByte(' ')
	b.WriteString(r.Message)
	for _, a := range h.attrs {
		writeAttr(&b, h.groups, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, h.groups, a)
		return true
	})
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	nh.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &nh
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	nh := *h
	nh.groups = append(append([]string{}, h.groups...), name)
	return &nh
}

// levelLabel renders a slog.Level as a short lowercase word.
func levelLabel(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "debug"
	case l < slog.LevelWarn:
		return "info"
	case l < slog.LevelError:
		return "warn"
	default:
		return "error"
	}
}

// writeAttr appends a single attribute as " key=value", prefixing the key with
// any active group names. Group attributes are flattened.
func writeAttr(b *strings.Builder, groups []string, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		nested := append(append([]string{}, groups...), a.Key)
		for _, ga := range v.Group() {
			writeAttr(b, nested, ga)
		}
		return
	}
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	b.WriteByte(' ')
	b.WriteString(key)
	b.WriteByte('=')
	b.WriteString(formatValue(v))
}

// formatValue renders a slog.Value, quoting it when it contains whitespace or
// quotes so the line stays parseable.
func formatValue(v slog.Value) string {
	s := v.String()
	if strings.ContainsAny(s, " \t\"\n") {
		return strconv.Quote(s)
	}
	return s
}
