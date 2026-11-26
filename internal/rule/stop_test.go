package rule

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func (d *daemons) processes() Processes {
	return Processes{
		Running: idle,
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
	caches := filepath.Join(temp(t), "caches")
	require.NoError(t, os.MkdirAll(filepath.Join(caches, "modules-2"), 0o755))
	write(t, filepath.Join(caches, "modules-2", "lib.jar"))
	return caches
}

func stopping(t *testing.T, caches string, p Processes) Rule {
	body := "describes: x\nroot: " + caches + "\nunless: [{running: [client], says: a client runs}]\nstop: {running: [daemon], idle-for: 2s, timeout: 10s, names: idle daemons, says: a daemon is using the CPU}\nremove: .\n"
	env := environment("/nowhere")
	env.Processes = p
	return parse(t, "020-gradle-caches.yaml", body).Rule(env)
}

func TestADryRunSaysItWouldStopAndDoesNotWait(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{pids: [][]string{{"41"}}, times: []string{"41 0:12.00"}}

	r, stdout, _ := run(stopping(t, caches, d.processes()), true)

	assert.DirExists(t, caches)
	assert.Equal(t, 1, r.Count)
	assert.Zero(t, d.terminated)
	assert.Contains(t, stdout, "WOULD STOP    idle daemons\n")
	assert.Equal(t, []time.Duration{2 * time.Second}, d.slept)
}

func TestDaemonsAreWaitedForUpToTheTimeoutEachSecond(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{pids: [][]string{{"41"}}, times: []string{"41 0:12.00"}}

	r, _, _ := run(stopping(t, caches, d.processes()), false)

	assert.DirExists(t, caches)
	assert.Equal(t, []string{caches + ": idle daemons did not stop within 10s"}, r.Failures)
	assert.Len(t, d.slept, 11)
}

func TestANewDaemonBetweenTheSnapshotsCountsAsBusy(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{
		pids:  [][]string{{"41"}, {"41", "42"}},
		times: []string{"41 0:12.00", "41 0:12.00\n42 0:00.00"},
	}

	r, _, stderr := run(stopping(t, caches, d.processes()), false)

	assert.DirExists(t, caches)
	assert.Zero(t, d.terminated)
	assert.Empty(t, r.Failures)
	assert.Contains(t, stderr, "a daemon is using the CPU")
}

func TestADaemonGoneBeforePsCountsAsNone(t *testing.T) {
	caches := newCaches(t)
	d := &daemons{pids: [][]string{{"41"}}, times: []string{""}}

	r, stdout, _ := run(stopping(t, caches, d.processes()), false)

	assert.NoDirExists(t, caches)
	assert.Equal(t, 1, r.Count)
	assert.Zero(t, d.terminated)
	assert.NotContains(t, stdout, "STOP")
}

func TestAProcessErrorWhileStoppingKeepsEverythingAndFails(t *testing.T) {
	cases := []struct {
		name   string
		d      daemons
		break_ func(*Processes)
		want   string
	}{
		{"pgrep", daemons{}, func(p *Processes) { p.Pids = func(string) ([]string, error) { return nil, errBroken } }, "pgrep -f 'daemon': exit status 2: illegal pattern"},
		{"ps", daemons{}, func(p *Processes) { p.CPUTimes = func([]string) (string, error) { return "", errBroken } }, "ps of idle daemons: exit status 2: illegal pattern"},
		{"pkill", daemons{}, func(p *Processes) { p.Terminate = func(string) error { return errBroken } }, "pkill -TERM -f 'daemon': exit status 2: illegal pattern"},
		{"busy check", daemons{}, func(p *Processes) { p.Running = func(string) (bool, error) { return false, errBroken } }, "pgrep -f 'client': exit status 2: illegal pattern"},
		{"second snapshot", daemons{timesFailAt: 2}, func(*Processes) {}, "ps of idle daemons: exit status 2: illegal pattern"},
		{"wait after SIGTERM", daemons{pidsFailAt: 3}, func(*Processes) {}, "pgrep -f 'daemon': exit status 2: illegal pattern"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caches := newCaches(t)
			d := tc.d
			d.pids = [][]string{{"41"}}
			d.times = []string{"41 0:12.00"}
			p := d.processes()
			tc.break_(&p)

			r, _, _ := run(stopping(t, caches, p), false)

			assert.DirExists(t, caches)
			assert.Zero(t, r.Count)
			assert.Equal(t, []string{tc.want}, r.Failures)
		})
	}
}
