//go:build !unix

package main

import "fmt"

func enableRaw(fd int) (interface{}, error) {
	return nil, fmt.Errorf("raw mode not supported on this platform")
}

func restoreTerm(fd int, state interface{}) error {
	return nil
}

func isTerminal(fd int) bool {
	return false
}
