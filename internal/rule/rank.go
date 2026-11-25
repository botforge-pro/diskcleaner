package rule

import (
	"cmp"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var version = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)

type member struct {
	name    string
	order   string
	present int
}

func (c *ctx) rank(r *Rank, all []found) ([]found, bool) {
	var spared map[string]bool
	if r.Spare.In != nil {
		names, ok := c.eval(*r.Spare.In, c.folder)
		if !ok {
			return nil, false
		}
		spared = map[string]bool{}
		for _, name := range names {
			spared[name] = true
		}
	}
	groups := map[string][]member{}
	read := func(v *Value, at string) (string, bool, bool) {
		if v == nil {
			return "", true, true
		}
		values, ok := c.eval(*v, at)
		if !ok {
			return "", false, false
		}
		if len(values) != 1 {
			return "", false, true
		}
		return values[0], true, true
	}
	for i, f := range all {
		group, set, ok := read(r.Per, f.path)
		if !ok {
			continue
		}
		order, ordered, ok := read(r.Order, f.path)
		if ok && set && ordered {
			groups[group] = append(groups[group], member{name: filepath.Base(f.path), order: order, present: i})
		}
	}
	for name := range spared {
		at := filepath.Join(c.folder, name)
		if slices.ContainsFunc(all, func(f found) bool { return f.path == at }) {
			continue
		}
		group, set, ok := read(r.Per, at)
		if !ok {
			continue
		}
		order, ordered, ok := read(r.Order, at)
		if ok && set && ordered {
			groups[group] = append(groups[group], member{name: name, order: order, present: -1})
		}
	}
	var goes []int
	for _, group := range slices.Sorted(maps.Keys(groups)) {
		goes = append(goes, c.cut(r, groups[group], spared)...)
	}
	slices.Sort(goes)
	going := make([]found, 0, len(goes))
	for _, i := range goes {
		going = append(going, all[i])
	}
	return going, true
}

func (c *ctx) cut(r *Rank, members []member, spared map[string]bool) []int {
	slices.SortStableFunc(members, func(a, b member) int {
		if r.Order == nil {
			return strings.Compare(a.name, b.name)
		}
		if by := compareValues(b.order, a.order); by != 0 {
			return by
		}
		return strings.Compare(a.name, b.name)
	})
	isSpared := func(m member) bool { return spared[m.name] }
	if r.Spare.Newest != "" {
		n := c.count(r.Spare.Newest)
		present := slices.DeleteFunc(slices.Clone(members), func(m member) bool { return m.present < 0 })
		var goes []int
		for _, m := range present[min(n, len(present)):] {
			goes = append(goes, m.present)
		}
		return goes
	}
	var deciders []member
	for _, m := range members {
		if isSpared(m) {
			deciders = append(deciders, m)
			if r.Order != nil {
				break
			}
		}
	}
	if len(deciders) == 0 {
		return nil
	}
	for _, d := range deciders {
		if d.present < 0 {
			c.r.Skip("%s: nothing here goes until %s is made", c.folder, d.name)
			return nil
		}
	}
	var goes []int
	for _, m := range members {
		switch {
		case m.present < 0 || isSpared(m):
		case r.Order == nil || compareValues(m.order, deciders[0].order) < 0:
			goes = append(goes, m.present)
		}
	}
	return goes
}

func compareValues(a, b string) int {
	if version.MatchString(a) && version.MatchString(b) {
		x, y := strings.Split(a, "."), strings.Split(b, ".")
		for i := range min(len(x), len(y)) {
			p, _ := strconv.Atoi(x[i])
			q, _ := strconv.Atoi(y[i])
			if by := cmp.Compare(p, q); by != 0 {
				return by
			}
		}
		return cmp.Compare(len(x), len(y))
	}
	if t, err := parseTime(a); err == nil {
		if u, err := parseTime(b); err == nil {
			return t.Compare(u)
		}
	}
	return strings.Compare(a, b)
}
