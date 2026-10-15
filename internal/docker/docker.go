package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const prune = "docker image prune --all --force"

type Step struct {
	Docker              string
	Data                string
	TimeoutSeconds      int
	PruneTimeoutSeconds int
	Backend             string
	Running             func(pattern string) (bool, error)
}

func Default(home string) (Step, error) {
	timeout, err := settings.PositiveDuration("DISKCLEANER_DOCKER_TIMEOUT", 30, time.Second, "seconds")
	if err != nil {
		return Step{}, err
	}
	pruneTimeout, err := settings.PositiveDuration("DISKCLEANER_DOCKER_PRUNE_TIMEOUT", 900, time.Second, "seconds")
	if err != nil {
		return Step{}, err
	}
	return Step{
		Docker:              "docker",
		Data:                settings.Env("DISKCLEANER_DOCKER_DATA", filepath.Join(home, "Library/Containers/com.docker.docker")),
		TimeoutSeconds:      timeout,
		PruneTimeoutSeconds: pruneTimeout,
		Backend:             settings.Env("DISKCLEANER_DOCKER_PROCESS", `com\.docker\.backend`),
		Running:             process.Running,
	}, nil
}

func (s Step) docker(seconds int, args ...string) process.Result {
	return process.RunGroup(seconds, s.Docker, args...)
}

func (s Step) Run(r *report.Report) {
	_, err := exec.LookPath(s.Docker)
	if errors.Is(err, exec.ErrNotFound) {
		return
	}
	if err != nil {
		r.Fail("look up %s: %v", s.Docker, err)
		return
	}
	fmt.Fprintln(r.Out, "DOCKER   images not used by any container")
	defer fmt.Fprintln(r.Out)

	info := s.docker(s.TimeoutSeconds, "info")
	switch {
	case info.TimedOut:
		r.Fail("docker info did not answer within %ds: the Docker daemon is hung, restart Docker Desktop", s.TimeoutSeconds)
		return
	case info.Leftover:
		r.Fail("docker info: exited but left a process running, which was stopped: %v", info.Err)
		return
	case info.Err != nil && !r.Busy(s.Running, s.Backend):
		r.Skip("docker images: Docker Desktop is not running")
		return
	case info.Err != nil:
		r.Fail("docker info: %v: %s", info.Err, info.Output())
		return
	}

	reclaimable, reclaimableKB, ok := s.reclaimable(r)
	if !ok || reclaimableKB == 0 {
		return
	}
	if r.DryRun {
		fmt.Fprintf(r.Out, "WOULD RUN     %s  (%s reclaimable)\n", prune, reclaimable)
		r.Count++
		r.KB += reclaimableKB
		return
	}

	beforeKB, measured := s.dataKB(r)
	fmt.Fprintf(r.Out, "RUN           %s  (%s reclaimable)\n", prune, reclaimable)
	pruned := s.docker(s.PruneTimeoutSeconds, "image", "prune", "--all", "--force")
	for _, line := range strings.Split(pruned.Stdout, "\n") {
		if strings.HasPrefix(line, "Total reclaimed space") {
			fmt.Fprintln(r.Out, line)
		}
	}
	switch {
	case pruned.TimedOut:
		r.Fail("docker image prune did not finish within %ds", s.PruneTimeoutSeconds)
	case pruned.Err != nil:
		r.Fail("docker image prune: %v: %s", pruned.Err, pruned.Output())
	default:
		r.Count++
	}
	afterKB, measuredAfter := s.dataKB(r)
	if measured && measuredAfter && afterKB < beforeKB {
		r.KB += beforeKB - afterKB
	}
}

func (s Step) reclaimable(r *report.Report) (string, int64, bool) {
	df := s.docker(s.TimeoutSeconds, "system", "df", "--format", "{{.Type}}\t{{.Reclaimable}}")
	switch {
	case df.TimedOut:
		r.Fail("docker system df did not answer within %ds", s.TimeoutSeconds)
		return "", 0, false
	case df.Err != nil:
		r.Fail("docker system df: %v: %s", df.Err, df.Output())
		return "", 0, false
	}
	for _, line := range strings.Split(df.Stdout, "\n") {
		kind, size, found := strings.Cut(line, "\t")
		if !found || kind != "Images" {
			continue
		}
		size, _, _ = strings.Cut(size, " (")
		if kb, ok := report.DecimalSizeKB(size); ok {
			return size, kb, true
		}
	}
	r.Fail("docker system df: cannot read the reclaimable size from: %s", df.Output())
	return "", 0, false
}

func (s Step) dataKB(r *report.Report) (int64, bool) {
	_, err := os.Lstat(s.Data)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, true
	}
	if err != nil {
		r.Fail("stat %s, the space Docker freed is not counted: %v", s.Data, err)
		return 0, false
	}
	kb, err := report.DiskUsageKB(s.Data)
	if err != nil {
		r.Fail("du %s, the space Docker freed is not counted: %v", s.Data, err)
		return 0, false
	}
	return kb, true
}
