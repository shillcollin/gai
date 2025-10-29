//go:build unix

package main

import "golang.org/x/sys/unix"

func enableRaw(fd int) (interface{}, error) {
	state, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}
	raw := *state
	raw.Iflag &^= unix.ISTRIP | unix.INLCR | unix.ICRNL | unix.IXON | unix.IXOFF
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ICANON | unix.ECHO | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return nil, err
	}
	return state, nil
}

func restoreTerm(fd int, state interface{}) error {
	termios, ok := state.(*unix.Termios)
	if !ok || termios == nil {
		return nil
	}
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, termios)
}

func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	return err == nil
}
