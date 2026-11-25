package rule

import "syscall"

const mntNoWait = 2

func mountPoints() (map[string]bool, error) {
	count, err := syscall.Getfsstat(nil, mntNoWait)
	if err != nil {
		return nil, err
	}
	table := make([]syscall.Statfs_t, count)
	count, err = syscall.Getfsstat(table, mntNoWait)
	if err != nil {
		return nil, err
	}
	points := make(map[string]bool, count)
	for _, entry := range table[:count] {
		points[cString(entry.Mntonname[:])] = true
	}
	return points, nil
}

func cString(raw []int8) string {
	name := make([]byte, 0, len(raw))
	for _, c := range raw {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return string(name)
}
