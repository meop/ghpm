package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"

	"github.com/meop/ghpm/internal/config"
)

var errSilent = errors.New("")

var defaultColorNames = map[string]color.Attribute{
	"black":   color.FgBlack,
	"red":     color.FgRed,
	"green":   color.FgGreen,
	"yellow":  color.FgYellow,
	"blue":    color.FgBlue,
	"magenta": color.FgMagenta,
	"cyan":    color.FgCyan,
	"white":   color.FgWhite,
}

// colorfn returns a sprint function for the named role, or nil if disabled/unknown.
func colorfn(cfg *config.Settings, role string) func(string) string {
	if cfg == nil || cfg.NoColor {
		return nil
	}
	name := ""
	if cfg.Color != nil {
		name = cfg.Color[role]
	}
	attr, ok := defaultColorNames[name]
	if !ok {
		return nil
	}
	fn := color.New(attr).SprintFunc()
	return func(s string) string { return fn(s) }
}

func printInfo(cfg *config.Settings, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if fn := colorfn(cfg, "info"); fn != nil {
		fmt.Println(fn("ℹ " + msg))
	} else {
		fmt.Println("ℹ " + msg)
	}
}

func printWarn(cfg *config.Settings, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if fn := colorfn(cfg, "warn"); fn != nil {
		fmt.Println(fn("⚠ " + msg))
	} else {
		fmt.Println("⚠ " + msg)
	}
}

func printFail(cfg *config.Settings, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if fn := colorfn(cfg, "fail"); fn != nil {
		fmt.Println(fn("✗ " + msg))
	} else {
		fmt.Println("✗ " + msg)
	}
}

func printPass(cfg *config.Settings, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if fn := colorfn(cfg, "pass"); fn != nil {
		fmt.Println(fn("✓ " + msg))
	} else {
		fmt.Println("✓ " + msg)
	}
}

// printTable prints a table with dynamic column widths.
// colColors is per-column; nil entry or nil slice means no color for that column.
// Colors are applied only to data rows, not headers or separator.
func printTable(headers []string, rows [][]string, colColors []func(string) string) {
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
				fmt.Print(" ")
			}
			if i < len(cells)-1 {
				fmt.Printf("%-*s", widths[i], cell)
			} else {
				fmt.Print(cell)
			}
		}
		fmt.Println()
	}

	prColored := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				fmt.Print(" ")
			}
			var fn func(string) string
			if colColors != nil && i < len(colColors) {
				fn = colColors[i]
			}
			if i < len(cells)-1 {
				pad := widths[i] - len(cell)
				if pad < 0 {
					pad = 0
				}
				if fn != nil {
					fmt.Print(fn(cell) + strings.Repeat(" ", pad))
				} else {
					fmt.Printf("%-*s", widths[i], cell)
				}
			} else {
				if fn != nil {
					fmt.Print(fn(cell))
				} else {
					fmt.Print(cell)
				}
			}
		}
		fmt.Println()
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
}
