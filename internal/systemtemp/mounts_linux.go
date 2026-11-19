package systemtemp

import (
	"os"
	"strings"
)

var unescape = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

func mountPoints() (map[string]bool, error) {
	table, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil, err
	}
	points := map[string]bool{}
	for _, line := range strings.Split(string(table), "\n") {
		if fields := strings.Fields(line); len(fields) > 1 {
			points[unescape.Replace(fields[1])] = true
		}
	}
	return points, nil
}
