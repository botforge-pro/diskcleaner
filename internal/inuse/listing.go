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

type Listing struct {
	lsof   Lsof
	every  int
	names  []string
	served int
}

func (l Lsof) Every(answers int) *Listing {
	return &Listing{lsof: l, every: answers}
}

func (l Lsof) Once() *Listing {
	return l.Every(0)
}

func (l *Listing) Lsof() Lsof {
	return l.lsof
}

func (l *Listing) Take() error {
	names, err := process.OpenNames(l.lsof.TimeoutSeconds, l.lsof.Command)
	if err != nil {
		return err
	}
	l.names, l.served = names, 0
	return nil
}

func (l *Listing) HoldsAny(paths ...string) (bool, error) {
	if l.names == nil || l.every > 0 && l.served >= l.every {
		if err := l.Take(); err != nil {
			return false, err
		}
	}
	l.served++
	return slices.ContainsFunc(paths, func(path string) bool { return process.Holds(l.names, path) }), nil
}
