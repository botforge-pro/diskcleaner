package lock

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

var ErrHeld = errors.New("another cleanup is already running")

var openFile = os.OpenFile

type Lock struct {
	file *os.File
}

func Take(path string, stderr io.Writer) (*Lock, error) {
	file, err := openFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o644)
	switch {
	case errors.Is(err, syscall.ENOSPC):
		fmt.Fprintf(stderr, "The disk is full, so the lock %s cannot be made; running without it.\n", path)
		return &Lock{}, nil
	case errors.Is(err, syscall.ELOOP):
		return nil, fmt.Errorf("%s is a link, as the shell version of diskcleaner left its lock; remove it once no cleanup runs, and run again", path)
	case err != nil:
		return nil, fmt.Errorf("cannot open the lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (lock %s); not starting a second one", ErrHeld, path)
		}
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}
