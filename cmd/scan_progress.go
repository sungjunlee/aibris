package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func printScanProgress(event types.ScanProgressEvent) {
	label := scanProgressLabel(event.Tool)
	switch event.State {
	case types.ScanProgressStart:
		fmt.Printf("  scanning %-12s\n", label)
	case types.ScanProgressDone:
		fmt.Printf("  found    %-12s %3d items   %s\n\n",
			label, event.Count, cleaner.FormatSize(event.Size))
	case types.ScanProgressError:
		fmt.Printf("  error    %-12s %s\n\n", label, event.Err)
	}
}

// scanProgressLabel is the human name for a provider in scan progress.
// The worktree adapter keeps Name() == "codex" for cache identity and
// --tool selectors; progress must not label every worktree row as Codex.
func scanProgressLabel(tool types.Tool) string {
	if tool == types.ToolCodex {
		return "worktree"
	}
	return string(tool)
}

type scanProgressPrinter struct {
	out         *os.File
	interactive bool
	mu          sync.Mutex
	stop        chan struct{}
	stopped     chan struct{}
	stopOnce    sync.Once
	active      map[types.Tool]bool
	started     int
	done        int
	items       int
	size        int64
	errors      int
	frame       int
}

func newScanProgressPrinter(out *os.File) *scanProgressPrinter {
	p := &scanProgressPrinter{
		out:         out,
		interactive: isTerminal(out),
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
		active:      make(map[types.Tool]bool),
	}
	if p.interactive {
		go p.spin()
	}
	return p
}

func (p *scanProgressPrinter) Handle(event types.ScanProgressEvent) {
	if !p.interactive {
		printScanProgress(event)
		return
	}

	p.mu.Lock()
	switch event.State {
	case types.ScanProgressStart:
		if !p.active[event.Tool] {
			p.started++
		}
		p.active[event.Tool] = true
	case types.ScanProgressDone:
		delete(p.active, event.Tool)
		p.done++
		p.items += event.Count
		p.size += event.Size
	case types.ScanProgressError:
		delete(p.active, event.Tool)
		p.done++
		p.errors++
	}
	p.renderLocked()
	p.mu.Unlock()
}

func (p *scanProgressPrinter) Stop() {
	if !p.interactive {
		return
	}

	p.stopOnce.Do(func() {
		close(p.stop)
		<-p.stopped
		p.mu.Lock()
		defer p.mu.Unlock()
		fmt.Fprint(p.out, "\r\x1b[2K")
		if p.started == 0 {
			return
		}
		if p.errors > 0 {
			fmt.Fprintf(p.out, "  scanned  %d sources   %d items   %s   %d errors\n\n",
				p.started, p.items, cleaner.FormatSize(p.size), p.errors)
			return
		}
		fmt.Fprintf(p.out, "  scanned  %d sources   %d items   %s\n\n",
			p.started, p.items, cleaner.FormatSize(p.size))
	})
}

func (p *scanProgressPrinter) spin() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer close(p.stopped)

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			p.frame++
			p.renderLocked()
			p.mu.Unlock()
		case <-p.stop:
			return
		}
	}
}

func (p *scanProgressPrinter) renderLocked() {
	if p.started == 0 {
		return
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	active := activeTools(p.active)
	if active == "" {
		if p.done >= p.started {
			return
		}
		active = "wrapping up"
	}
	fmt.Fprintf(p.out, "\r\x1b[2K  %s scanning %s   %d/%d done   %d items   %s",
		frames[p.frame%len(frames)], active, p.done, p.started, p.items, cleaner.FormatSize(p.size))
}

func activeTools(active map[types.Tool]bool) string {
	if len(active) == 0 {
		return ""
	}
	tools := make([]string, 0, len(active))
	for tool := range active {
		tools = append(tools, scanProgressLabel(tool))
	}
	sort.Strings(tools)
	if len(tools) > 3 {
		return strings.Join(tools[:3], ", ") + "..."
	}
	return strings.Join(tools, ", ")
}
