package inuse

import (
	"slices"

	"github.com/botforge-pro/diskcleaner/internal/process"
)

type Lsof struct {
	Command        string
	TimeoutSeconds int
}

func DefaultLsof() Lsof {
	return Lsof{Command: "lsof", TimeoutSeconds: 30}
}

type Snapshot struct {
	names []string
}

func (l Lsof) Take() (Snapshot, error) {
	names, err := process.OpenNames(l.TimeoutSeconds, l.Command)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{names: names}, nil
}

func (s Snapshot) Holds(paths ...string) bool {
	return slices.ContainsFunc(paths, func(path string) bool { return process.Holds(s.names, path) })
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
