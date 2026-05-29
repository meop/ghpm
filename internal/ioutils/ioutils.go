package ioutils

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

var Stdin = bufio.NewReader(os.Stdin)

var ErrSkip = errors.New("skipped")

func ReadLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := Stdin.ReadString('\n')
	return strings.TrimSpace(line)
}

// ReadSingle reads a single-item selection.
// Empty input selects item 1. Entering 0 or invalid input returns ErrSkip.
func ReadSingle(label string) (int, error) {
	line := ReadLine(label + " [empty=1] (0=skip): ")
	if line == "" {
		return 1, nil
	}
	var idx int
	if _, err := fmt.Sscanf(line, "%d", &idx); err != nil || idx == 0 {
		return 0, ErrSkip
	}
	return idx, nil
}
