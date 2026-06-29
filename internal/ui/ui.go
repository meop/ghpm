// Package ui is the single sink for all console output and prompts. It uses
// deferred separators: Break requests a blank line, but the blank is only
// emitted immediately before the next real output. This makes blank-line
// placement robust — a Break before an iteration that prints nothing leaves no
// stray blank, consecutive Breaks never stack into a double blank, and a Break
// with nothing after it produces no trailing blank. Both output and prompt
// reads flow through here so the deferred separator is honored across the
// output/input seam (e.g. a menu's read prompt).
package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

// ErrSkip signals the user declined a selection prompt.
var ErrSkip = errors.New("skipped")

var (
	out           io.Writer = os.Stdout
	in                      = bufio.NewReader(os.Stdin)
	started       bool      // anything emitted yet
	pending       bool      // a separator was requested
	colorResolver func(role string) func(string) string
	// mu guards the separator state and writes so non-prompt output emitted from
	// parallel workers (e.g. download progress) cannot interleave or race the
	// deferred-separator check. Held only around the output portion of a read,
	// never across the blocking stdin read itself.
	mu sync.Mutex
)

// SetOutput redirects output and clears separator state (tests).
func SetOutput(w io.Writer) {
	out = w
	started = false
	pending = false
}

// SetInput redirects the prompt reader (tests).
func SetInput(r io.Reader) { in = bufio.NewReader(r) }

// SetColorResolver installs the function mapping a role ("warn", "fail",
// "pass") to a colorizer, or nil to disable color for that role.
func SetColorResolver(fn func(role string) func(string) string) { colorResolver = fn }

// Reset clears separator state without changing the output target (tests).
func Reset() {
	started = false
	pending = false
}

// Break requests a blank line before the next output. It is deferred and
// idempotent: it does nothing until real output follows, and repeated calls
// collapse to a single blank.
func Break() {
	mu.Lock()
	defer mu.Unlock()
	if started {
		pending = true
	}
}

// w-prefixed helpers write to out, ignoring write errors (stdout writes do not
// meaningfully fail, and the original stdout-based code ignored them too).
func wln(args ...any)               { _, _ = fmt.Fprintln(out, args...) }
func wf(format string, args ...any) { _, _ = fmt.Fprintf(out, format, args...) }
func ws(s string)                   { _, _ = fmt.Fprint(out, s) }

// flush emits a deferred blank if one is pending. Callers must hold mu.
func flush() {
	if pending {
		wln()
		pending = false
	}
}

func line(s string) {
	mu.Lock()
	defer mu.Unlock()
	flush()
	wln(s)
	started = true
}

// Out prints a plain, undecorated line, honoring any pending Break.
func Out(format string, args ...any) { line(fmt.Sprintf(format, args...)) }

func decorated(role, prefix, format string, args ...any) {
	msg := prefix + fmt.Sprintf(format, args...)
	if colorResolver != nil {
		if fn := colorResolver(role); fn != nil {
			msg = fn(msg)
		}
	}
	line(msg)
}

// Warn, Fail, and Pass print a role-prefixed, optionally colored line.
func Warn(format string, args ...any) { decorated("warn", "‼ ", format, args...) }
func Fail(format string, args ...any) { decorated("fail", "✗ ", format, args...) }
func Pass(format string, args ...any) { decorated("pass", "✓ ", format, args...) }

// ReadLine flushes any pending Break, prints prompt (no trailing newline), and
// returns the user's trimmed input line.
func ReadLine(prompt string) string {
	mu.Lock()
	flush()
	ws(prompt)
	started = true
	mu.Unlock()
	s, _ := in.ReadString('\n')
	return strings.TrimSpace(s)
}

// ReadSingle reads a single-item selection. Empty input selects item 1;
// entering 0 or any input that is not a single positive integer returns
// ErrSkip. The whole line must parse — "2 5" is rejected as garbage, not
// salvaged to 2 — matching parseMultiSelect's whole-line validation so every
// prompt swallows unrecognized input to the same safe default.
func ReadSingle(label string) (int, error) {
	s := ReadLine(label + " [empty=1] (0=skip): ")
	if s == "" {
		return 1, nil
	}
	idx, err := strconv.Atoi(s)
	if err != nil || idx <= 0 {
		return 0, ErrSkip
	}
	return idx, nil
}

// Prompt brackets an interactive prompt so it forms its own block: a blank line
// before and after. Both are deferred Breaks, so a prompt at the very start or
// end of output produces no stray blank, and two adjacent prompts collapse to a
// single blank between them. The selection menus (asset/config) run through here,
// so blank-line placement around them lives in exactly one place. Confirm is the
// deliberate exception — it brackets with a leading Break only (see Confirm).
// body performs the prompt's prints and reads —
// including any multi-step reads — so the trailing blank lands after all of them.
func Prompt[T any](body func() (T, error)) (T, error) {
	Break()
	v, err := body()
	Break()
	return v, err
}

// Menu prints a selection menu: the header (prefixed with "<label>: " when label
// is non-empty so the prompt names its package) followed by each pre-formatted
// item as a numbered line. It is meant to be called at the top of a Prompt body.
func Menu(label, header string, items []string) {
	if label != "" {
		header = label + ": " + header
	}
	Out("%s", header)
	for i, item := range items {
		Out("  %d) %s", i+1, item)
	}
}

// Confirm prints "msg [y,[n]]: " with a blank line before it but NOT after, and
// returns true for empty, "y", or "yes". Unlike the selection menus, a confirm
// is the bail point of a gate: whatever follows (the operation's progress lines,
// or `add`'s shim creation) is the work the user just opted into, so it nests
// tight under the confirm with no separating blank. The leading Break still
// flushes any pending separator before the prompt; it just doesn't request a
// trailing one, so it does not route through Prompt.
func Confirm(msg string) bool {
	Break()
	s := strings.ToLower(ReadLine(msg + " [y,[n]]: "))
	return s == "" || s == "y" || s == "yes"
}

// Table renders a space-aligned table as a single block preceded by a Break.
// colColors, when non-nil, colorizes each column by index.
func Table(headers []string, rows [][]string, colColors []func(string) string) {
	mu.Lock()
	defer mu.Unlock()
	if started {
		pending = true
	}
	flush()

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i := range headers {
			if i < len(row) && len(row[i]) > widths[i] {
				widths[i] = len(row[i])
			}
		}
	}

	prRaw := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				ws(" ")
			}
			if i < len(cells)-1 {
				wf("%-*s", widths[i], cell)
			} else {
				ws(cell)
			}
		}
		wln()
	}

	prColored := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				ws(" ")
			}
			var fn func(string) string
			if colColors != nil && i < len(colColors) {
				fn = colColors[i]
			}
			if i < len(cells)-1 {
				pad := max(widths[i]-len(cell), 0)
				if fn != nil {
					ws(fn(cell) + strings.Repeat(" ", pad))
				} else {
					wf("%-*s", widths[i], cell)
				}
			} else {
				if fn != nil {
					ws(fn(cell))
				} else {
					ws(cell)
				}
			}
		}
		wln()
	}

	dashes := make([]string, len(headers))
	for i, h := range headers {
		dashes[i] = strings.Repeat("-", len(h))
	}
	prRaw(headers)
	prRaw(dashes)
	for _, row := range rows {
		prColored(row)
	}
	started = true
}
