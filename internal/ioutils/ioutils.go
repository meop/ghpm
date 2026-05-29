package ioutils

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func ReadLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
