package asset

import (
	"fmt"

	"github.com/meop/ghpm/internal/ioutils"
)

// readSingle reads a single-item selection.
// Empty input selects item 1. Entering 0 or invalid input returns ErrSkip.
func readSingle() (int, error) {
	return ioutils.ReadSingle("enter number")
}

// readMultiFirstWithShowMore is like readMultiAll but accepts a separate parse
// selects item 1 instead of all items.
func readMultiFirstWithShowMore(promptN, parseN int) ([]int, error) {
	line := ioutils.ReadLine(fmt.Sprintf("enter number(s) [empty=1] (0=skip | 1[,][-]%d): ", promptN))
	if line == "" {
		return []int{1}, nil
	}
	indices, err := parseMultiSelect(line, parseN)
	if err != nil || indices == nil {
		return nil, ErrSkip
	}
	return indices, nil
}

// readMultiAll reads a multi-select prompt where empty input selects all items.
// Entering 0 or invalid input returns ErrSkip.
func readMultiAll(n int) ([]int, error) {
	line := ioutils.ReadLine(fmt.Sprintf("enter number(s) [empty=all] (0=skip | 1[,][-]%d): ", n))
	indices, err := parseMultiSelect(line, n)
	if err != nil || indices == nil {
		return nil, ErrSkip
	}
	return indices, nil
}

// readMultiFirst reads a multi-select prompt where empty input selects item 1.
// Entering 0 or invalid input returns ErrSkip.
func readMultiFirst(n int) ([]int, error) {
	line := ioutils.ReadLine(fmt.Sprintf("enter number(s) [empty=1] (0=skip | 1[,][-]%d): ", n))
	if line == "" {
		return []int{1}, nil
	}
	indices, err := parseMultiSelect(line, n)
	if err != nil || indices == nil {
		return nil, ErrSkip
	}
	return indices, nil
}

// readMultiOptional reads a multi-select prompt for an optional action (e.g. "to rename").
// Empty or invalid input returns nil indices with no error (no items selected, proceed).
// Entering 0 returns ErrSkip.
func readMultiOptional(action string, n int) ([]int, error) {
	line := ioutils.ReadLine(fmt.Sprintf("enter number(s) %s [empty=skip] (0=skip | 1[,][-]%d): ", action, n))
	if line == "" {
		return nil, nil
	}
	indices, err := parseMultiSelect(line, n)
	if err != nil {
		return nil, ErrSkip
	}
	if indices == nil {
		return nil, ErrSkip
	}
	return indices, nil
}
