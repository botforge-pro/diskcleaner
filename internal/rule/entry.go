package rule

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const openFolder = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

var errMounted = errors.New("a mounted volume is on its way")

type entry struct {
	parent int
	name   string
	stat   unix.Stat_t
}

func openEntry(dir, path string, volume int64) (entry, error) {
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
		return entry{}, noFollow(err)
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
			return entry{}, errMounted
		}
	}
	e := entry{parent: fd, name: parts[len(parts)-1]}
	if err := unix.Fstatat(fd, e.name, &e.stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		_ = unix.Close(fd)
		return entry{}, err
	}
	if int64(e.stat.Dev) != volume {
		_ = unix.Close(fd)
		return entry{}, errMounted
	}
	return e, nil
}

func noFollow(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return errors.New("a folder on its way is no longer a folder")
	}
	return err
}

func (e entry) close() {
	_ = unix.Close(e.parent)
}

func removeTree(parent int, name string, volume uint64) error {
	fd, err := unix.Openat(parent, name, openFolder, 0)
	if err != nil {
		return noFollow(err)
	}
	folder := os.NewFile(uintptr(fd), name)
	defer folder.Close()
	var self unix.Stat_t
	if err := unix.Fstat(fd, &self); err != nil {
		return err
	}
	if uint64(self.Dev) != volume {
		return errMounted
	}
	names, err := folder.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, child := range names {
		var stat unix.Stat_t
		err := unix.Fstatat(fd, child, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			err = removeTree(fd, child, volume)
		} else {
			err = unix.Unlinkat(fd, child, 0)
		}
		if err != nil && !errors.Is(err, unix.ENOENT) {
			return err
		}
	}
	return unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
}
