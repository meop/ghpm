package asset

import (
	"fmt"

	"github.com/meop/ghpm/internal/ioutils"
)

// readSingleFirst reads a single-item selection.
// Empty input selects item 1. Entering 0 or invalid input returns ErrSkip.
func readSingleFirst() (int, error) {
	line := ioutils.ReadLine("enter number [empty=1] (0=skip): ")
	if line == "" {
		return 1, nil
	}
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx == 0 {
		return 0, ErrSkip
	}
	return idx, nil
}

// readSingle reads a single-item selection with no default for empty input.
// Entering 0 returns ErrSkip. Empty or invalid input returns an error.
func readSingle() (int, error) {
	return ioutils.ReadSingle("enter number")
}

// readMultiAllWithShowMore is like readMultiAll but accepts a separate parse
// maximum. The range hint displays promptN while selections up to parseN are
// accepted — used when a synthetic "show more" entry sits above the visible range.
func readMultiAllWithShowMore(promptN, parseN int) ([]int, error) {
	line := ioutils.ReadLine(fmt.Sprintf("enter number(s) [empty=all] (0=skip | 1[,][-]%d): ", promptN))
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
