package systemtemp

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const openFolder = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

type entry struct {
	parent int
	name   string
	stat   unix.Stat_t
}

func openEntry(dir, path string, volume int64, mounts map[string]bool) (entry, error) {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return entry{}, err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if rel == "." || parts[0] == ".." {
		return entry{}, fmt.Errorf("not inside %s", dir)
	}
	fd, err := unix.Open(dir, openFolder, 0)
	if err != nil {
		return entry{}, err
	}
	for i := range parts {
		if mounts[filepath.Join(dir, filepath.Join(parts[:i+1]...))] {
			_ = unix.Close(fd)
			return entry{}, errors.New("a mounted volume is on its way")
		}
	}
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(fd, part, openFolder, 0)
		_ = unix.Close(fd)
		if err != nil {
			return entry{}, noFollow(err)
		}
		fd = next
		var folder unix.Stat_t
		if err := unix.Fstat(fd, &folder); err != nil {
			_ = unix.Close(fd)
			return entry{}, err
		}
		if int64(folder.Dev) != volume {
			_ = unix.Close(fd)
			return entry{}, errors.New("a mounted volume is on its way")
		}
	}
	e := entry{parent: fd, name: parts[len(parts)-1]}
	if err := unix.Fstatat(fd, e.name, &e.stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		_ = unix.Close(fd)
		return entry{}, err
	}
	if int64(e.stat.Dev) != volume {
		_ = unix.Close(fd)
		return entry{}, errors.New("now on another volume")
	}
	return e, nil
}

func noFollow(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return errors.New("a folder on its way is no longer a folder")
	}
	return err
}

func (e entry) remove(isDir bool) error {
	flags := 0
	if isDir {
		flags = unix.AT_REMOVEDIR
	}
	return unix.Unlinkat(e.parent, e.name, flags)
}

func (e entry) close() {
	_ = unix.Close(e.parent)
}
