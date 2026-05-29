package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func readYN() bool {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}

func promptConfirm(msg string) bool {
	if yes {
		return true
	}
	fmt.Printf("%s [y,[n]]: ", msg)
	return readYN()
}
