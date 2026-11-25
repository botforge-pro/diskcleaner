package inuse

import (
	"os"
	"slices"
	"syscall"

	"github.com/botforge-pro/diskcleaner/internal/process"
)

type Lsof struct {
	Command        string
	TimeoutSeconds int
}

func DefaultLsof() Lsof {
	return Lsof{Command: "lsof", TimeoutSeconds: 30}
}

type identity struct {
	device, inode uint64
}

type Snapshot struct {
	names []string
	files map[identity]bool
}

func (l Lsof) Take() (Snapshot, error) {
	open, err := process.OpenFiles(l.TimeoutSeconds, l.Command)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{files: map[identity]bool{}}
	for _, file := range open {
		s.names = append(s.names, file.Name)
		if file.Inode != 0 {
			s.files[identity{file.Device, file.Inode}] = true
		}
	}
	return s, nil
}

func (s Snapshot) Holds(paths ...string) bool {
	return slices.ContainsFunc(paths, func(path string) bool { return process.Holds(s.names, path) || s.holdsFile(path) })
}

func (s Snapshot) holdsFile(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.files[identity{uint64(stat.Dev), stat.Ino}]
}

type Listing struct {
	lsof   Lsof
	every  int
	latest *Snapshot
	served int
}

func (l Lsof) Every(answers int) *Listing {
	return &Listing{lsof: l, every: max(answers, 1)}
}

func (l Lsof) Once() *Listing {
	return &Listing{lsof: l}
}

func (l *Listing) Lsof() Lsof {
	return l.lsof
}

func (l *Listing) HoldsAny(paths ...string) (bool, error) {
	if l.latest == nil || l.every > 0 && l.served >= l.every {
		snapshot, err := l.lsof.Take()
		if err != nil {
			return false, err
		}
		l.latest, l.served = &snapshot, 0
	}
	l.served++
	return l.latest.Holds(paths...), nil
}
