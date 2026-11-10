package lock

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
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
	case err != nil:
		return nil, fmt.Errorf("cannot open the lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder, _ := io.ReadAll(file)
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s, lock %s); not starting a second one", ErrHeld, holderOf(holder), path)
		}
		return nil, fmt.Errorf("cannot lock %s: %w", path, err)
	}
	if err := file.Truncate(0); err == nil {
		fmt.Fprintf(file, "pid %d since %s", os.Getpid(), time.Now().Format(time.RFC3339))
	}
	return &Lock{file: file}, nil
}

func holderOf(content []byte) string {
	if holder := strings.TrimSpace(string(content)); holder != "" {
		return holder
	}
	return "holder unknown"
}

func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	return l.file.Close()
}
