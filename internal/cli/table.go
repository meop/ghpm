package cli

import (
	"errors"

	"github.com/fatih/color"

	"github.com/meop/ghpm/internal/config"
	"github.com/meop/ghpm/internal/ui"
)

var errSilent = errors.New("")

// sep requests a deferred blank line before the next output. See internal/ui.
func sep() { ui.Break() }

func print(format string, args ...any) {
	if quiet {
		return
	}
	ui.Out(format, args...)
}

func printInfo(_ *config.Settings, format string, args ...any) {
	if quiet {
		return
	}
	ui.Info(format, args...)
}

func printWarn(_ *config.Settings, format string, args ...any) {
	if quiet {
		return
	}
	ui.Warn(format, args...)
}

func printFail(_ *config.Settings, format string, args ...any) { ui.Fail(format, args...) }

func printPass(_ *config.Settings, format string, args ...any) { ui.Pass(format, args...) }

func printTable(headers []string, rows [][]string, colColors []func(string) string) {
	ui.Table(headers, rows, colColors)
}

// gate renders the "here's what I'm going to do" preview table and then, unless
// this is a dry run, asks confirmMsg. It returns true only when the user opts in;
// both a dry run and a declined prompt return false, signaling the caller to stop.
// This is the single gate that every mutating multi-package command (add, sync,
// download, upgrade) shows before doing work.
func gate(headers []string, rows [][]string, colColors []func(string) string, confirmMsg string) bool {
	printTable(headers, rows, colColors)
	if dryRun {
		return false
	}
	return promptConfirm(confirmMsg)
}

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
