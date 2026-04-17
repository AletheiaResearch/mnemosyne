package cli

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/mattn/go-isatty"
)

type extractProgress struct {
	records      atomic.Int64
	redactions   atomic.Int64
	selectionsOK atomic.Int64
	totalSel     int64
}

func newExtractProgress(total int) *extractProgress {
	return &extractProgress{totalSel: int64(total)}
}

func (p *extractProgress) addRecord(redactions int) {
	p.records.Add(1)
	p.redactions.Add(int64(redactions))
}

func (p *extractProgress) completeSelection() {
	p.selectionsOK.Add(1)
}

// run emits progress to w every ~150ms on a TTY; no-op otherwise.
// Stop must be called when extraction finishes (before the final JSON
// summary is printed) so the progress line is cleared.
func (p *extractProgress) run(w io.Writer) (stop func()) {
	f, ok := w.(*os.File)
	if !ok || !isatty.IsTerminal(f.Fd()) {
		return func() {}
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	ticker := time.NewTicker(150 * time.Millisecond)

	go func() {
		defer close(stopped)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				clearLine(f)
				return
			case <-ticker.C:
				p.render(f)
			}
		}
	}()

	return func() {
		close(done)
		<-stopped
	}
}

func (p *extractProgress) render(w io.Writer) {
	done := p.selectionsOK.Load()
	total := p.totalSel
	records := p.records.Load()
	redactions := p.redactions.Load()
	fmt.Fprintf(w, "\r\x1b[2K[%d/%d selections] %d records · %s redactions",
		done, total, records, formatCount(redactions))
}

func clearLine(w io.Writer) {
	fmt.Fprint(w, "\r\x1b[2K")
}

func formatCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}
