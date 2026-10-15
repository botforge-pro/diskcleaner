package gradle

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type daemons struct {
	pids        [][]string
	times       []string
	pidsFailAt  int
	timesFailAt int
	pidsCalls   int
	timesCalls  int
	terminated  int
	slept       []time.Duration
}

var errBroken = errors.New("exit status 2: illegal pattern")

func (d *daemons) step(caches string) Step {
	return Step{
		Caches:  caches,
		Daemons: "daemon",
		Clients: "client",
		Running: func(string) (bool, error) { return false, nil },
		Pids: func(string) ([]string, error) {
			d.pidsCalls++
			if d.pidsCalls == d.pidsFailAt {
				return nil, errBroken
			}
			if len(d.pids) == 0 {
				return nil, nil
			}
			pids := d.pids[0]
			if len(d.pids) > 1 {
				d.pids = d.pids[1:]
			}
			return pids, nil
		},
		CPUTimes: func(pids []string) (string, error) {
			d.timesCalls++
			if d.timesCalls == d.timesFailAt {
				return "", errBroken
			}
			if len(pids) == 0 {
				return "", nil
			}
			times := d.times[0]
			if len(d.times) > 1 {
				d.times = d.times[1:]
			}
			return times, nil
		},
		Terminate: func(string) error {
			d.terminated++
			return nil
		},
		Sleep: func(duration time.Duration) { d.slept = append(d.slept, duration) },
	}
}

func newCaches(t *testing.T) string {
	caches := filepath.Join(t.TempDir(), ".gradle", "caches")
	path := filepath.Join(caches, "modules-2", "lib.jar")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o644))
	return caches
}

func run(s Step, dryRun bool) (*report.Report, string, string) {
	var stdout, stderr bytes.Buffer
	r := &report.Report{Out: &stdout, Err: &stderr, DryRun: dryRun}
	s.Run(r)
	return r, stdout.String(), stderr.String()
}

func TestRemovesCachesWithoutDaemons(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{}

	r, stdout, stderr := run(d.step(caches), false)

	assert.Empty(t, stderr)
	assert.NoDirExists(t, caches)
	assert.Equal(t, 1, r.Count)
	assert.Contains(t, stdout, "GRADLE   "+caches+"\n")
	assert.NotContains(t, stdout, "STOP")
	assert.Empty(t, d.slept)
}

func TestStopsIdleDaemonsBeforeRemoving(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{
		pids:  [][]string{{"41"}, {"41"}, nil},
		times: []string{"41 0:12.00", "41 0:12.00"},
	}

	r, stdout, _ := run(d.step(caches), false)

	assert.NoDirExists(t, caches)
	assert.Equal(t, 1, r.Count)
	assert.Equal(t, 1, d.terminated)
	assert.Equal(t, []time.Duration{2 * time.Second}, d.slept)
	assert.Contains(t, stdout, "STOP          idle Gradle and Kotlin daemons\n")
}

func TestSkipsWhileADaemonUsesTheCPU(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{
		pids:  [][]string{{"41"}},
		times: []string{"41 0:12.00", "41 0:12.40"},
	}

	r, _, stderr := run(d.step(caches), false)

	assert.DirExists(t, caches)
	assert.Zero(t, r.Count)
	assert.Empty(t, r.Failures)
	assert.Zero(t, d.terminated)
	assert.Contains(t, stderr, "SKIPPED       "+caches+": a Gradle or Kotlin daemon is using the CPU\n")
}

func TestSkipsWhileAClientRuns(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{}
	s := d.step(caches)
	s.Running = func(string) (bool, error) { return true, nil }

	r, _, stderr := run(s, false)

	assert.DirExists(t, caches)
	assert.Zero(t, r.Count)
	assert.Contains(t, stderr, "SKIPPED       "+caches+": a Gradle build or Android Studio / IntelliJ IDEA is running\n")
}

func TestDryRunStopsAndRemovesNothing(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{
		pids:  [][]string{{"41"}},
		times: []string{"41 0:12.00"},
	}

	r, stdout, _ := run(d.step(caches), true)

	assert.DirExists(t, caches)
	assert.Equal(t, 1, r.Count)
	assert.Zero(t, d.terminated)
	assert.Contains(t, stdout, "WOULD STOP    idle Gradle and Kotlin daemons\n")
	assert.Contains(t, stdout, "WOULD REMOVE")
}

func TestDaemonsThatDoNotStopKeepTheCaches(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{
		pids:  [][]string{{"41"}},
		times: []string{"41 0:12.00"},
	}

	r, _, _ := run(d.step(caches), false)

	assert.DirExists(t, caches)
	assert.Zero(t, r.Count)
	require.Len(t, r.Failures, 1)
	assert.Equal(t, caches+": Gradle or Kotlin daemons did not stop within 10s", r.Failures[0])
	assert.Len(t, d.slept, 11)
}

func TestANewDaemonBetweenTheSnapshotsSkips(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{
		pids:  [][]string{{"41"}, {"41", "42"}},
		times: []string{"41 0:12.00", "41 0:12.00\n42 0:00.00"},
	}

	r, _, stderr := run(d.step(caches), false)

	assert.DirExists(t, caches)
	assert.Zero(t, d.terminated)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stderr, "a Gradle or Kotlin daemon is using the CPU")
}

func TestADaemonGoneBeforePsCountsAsNoDaemon(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{
		pids:  [][]string{{"41"}},
		times: []string{""},
	}

	r, stdout, _ := run(d.step(caches), false)

	assert.NoDirExists(t, caches)
	assert.Equal(t, 1, r.Count)
	assert.Zero(t, d.terminated)
	assert.NotContains(t, stdout, "STOP")
}

func TestLaterProcessErrorsKeepTheCachesAndFail(t *testing.T) {
	cases := []struct {
		name string
		d    daemons
		want string
	}{
		{"second snapshot", daemons{timesFailAt: 2}, "ps of the Gradle and Kotlin daemons: exit status 2: illegal pattern"},
		{"wait after SIGTERM", daemons{pidsFailAt: 3}, "pgrep -f 'daemon': exit status 2: illegal pattern"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caches := newCaches(t)
			d := tc.d
			d.pids = [][]string{{"41"}}
			d.times = []string{"41 0:12.00"}

			r, _, _ := run(d.step(caches), false)

			assert.DirExists(t, caches)
			assert.Zero(t, r.Count)
			require.Len(t, r.Failures, 1)
			assert.Equal(t, tc.want, r.Failures[0])
		})
	}
}

func TestProcessErrorsKeepTheCachesAndFail(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(*Step)
		want   string
	}{
		{"pgrep", func(s *Step) { s.Pids = func(string) ([]string, error) { return nil, errBroken } }, "pgrep -f 'daemon': exit status 2: illegal pattern"},
		{"ps", func(s *Step) { s.CPUTimes = func([]string) (string, error) { return "", errBroken } }, "ps of the Gradle and Kotlin daemons: exit status 2: illegal pattern"},
		{"pkill", func(s *Step) { s.Terminate = func(string) error { return errBroken } }, "pkill -TERM -f 'daemon': exit status 2: illegal pattern"},
		{"client check", func(s *Step) { s.Running = func(string) (bool, error) { return false, errBroken } }, "pgrep -f 'client': exit status 2: illegal pattern"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caches := newCaches(t)
			d := &daemons{
				pids:  [][]string{{"41"}},
				times: []string{"41 0:12.00"},
			}
			s := d.step(caches)
			tc.break_(&s)

			r, _, _ := run(s, false)

			assert.DirExists(t, caches)
			assert.Zero(t, r.Count)
			require.Len(t, r.Failures, 1)
			assert.Equal(t, tc.want, r.Failures[0])
		})
	}
}

func TestMissingCachesAreQuiet(t *testing.T) {
	d := &daemons{}

	r, stdout, stderr := run(d.step(filepath.Join(t.TempDir(), "absent")), false)

	assert.Zero(t, r.Count)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestDefaultFollowsGradleUserHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRADLE_USER_HOME", filepath.Join(home, "gradle-home"))

	assert.Equal(t, filepath.Join(home, "gradle-home", "caches"), Default(home).Caches)
}
