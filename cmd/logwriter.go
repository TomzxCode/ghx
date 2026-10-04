package cmd

import (
	"io"
	"os"
	"sync"

	"github.com/schollz/progressbar/v3"
)

// activeBar is the progress bar currently rendering to stderr, if any. When
// set, log output is routed through logWriter so a log line never lands in the
// middle of a rendered bar.
var (
	activeBarMu sync.Mutex
	activeBar   *progressbar.ProgressBar
)

// setActiveBar registers (or clears, when nil) the progress bar that log
// writes must clear before printing.
func setActiveBar(bar *progressbar.ProgressBar) {
	activeBarMu.Lock()
	activeBar = bar
	activeBarMu.Unlock()
}

// logWriter is the destination for logger output. When a progress bar is
// active it clears the bar, writes the log line, then redraws the bar so the
// two never interleave on the terminal. Without an active bar it is a plain
// stderr write.
type logWriter struct{ out io.Writer }

func (w logWriter) Write(p []byte) (int, error) {
	activeBarMu.Lock()
	bar := activeBar
	activeBarMu.Unlock()

	if bar == nil {
		return w.out.Write(p)
	}
	// Clear the current bar line, emit the log line, then redraw the bar.
	_ = bar.Clear()
	n, err := w.out.Write(p)
	_ = bar.RenderBlank()
	return n, err
}

// osStderrLogWriter is the writer handed to the logging backend.
func osStderrLogWriter() io.Writer { return logWriter{out: os.Stderr} }
