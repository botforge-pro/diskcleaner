package docker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const prune = "docker image prune --all --force"

var humanSize = regexp.MustCompile(`^([0-9.]+) ?([kKMGT]?B)$`)

var bytesPerUnit = map[string]float64{
	"B":  1,
	"kB": 1e3,
	"KB": 1e3,
	"MB": 1e6,
	"GB": 1e9,
	"TB": 1e12,
}

const maxTimeoutSeconds = math.MaxInt64 / int64(time.Second)

type Step struct {
	Docker              string
	Data                string
	TimeoutSeconds      int
	PruneTimeoutSeconds int
	Backend             string
	Running             func(pattern string) (bool, error)
}

func Default(home string) (Step, error) {
	timeout, err := timeoutSetting("DISKCLEANER_DOCKER_TIMEOUT", 30)
	if err != nil {
		return Step{}, err
	}
	pruneTimeout, err := timeoutSetting("DISKCLEANER_DOCKER_PRUNE_TIMEOUT", 900)
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

func timeoutSetting(name string, fallback int) (int, error) {
	seconds, err := settings.PositiveInt(name, fallback)
	if err != nil {
		return 0, err
	}
	if int64(seconds) > maxTimeoutSeconds {
		return 0, fmt.Errorf("%s must be at most %d seconds, got: %d", name, maxTimeoutSeconds, seconds)
	}
	return seconds, nil
}

type result struct {
	stdout   string
	stderr   string
	timedOut bool
	leftover bool
	err      error
}

func (res result) output() string {
	return strings.TrimSpace(strings.TrimSpace(res.stdout) + "\n" + strings.TrimSpace(res.stderr))
}

func (s Step) docker(seconds int, args ...string) result {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(ctx, s.Docker, args...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	err := command.Run()
	res := result{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
	if command.Process != nil {
		group := -command.Process.Pid
		probe := syscall.Kill(group, 0)
		res.leftover = probe == nil || errors.Is(probe, syscall.EPERM)
		if res.leftover {
			res.err = errors.Join(err, errors.New("it left a process running"))
			if killErr := syscall.Kill(group, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
				res.err = errors.Join(res.err, fmt.Errorf("stop its leftover processes: %w", killErr))
			}
		}
	}
	res.timedOut = err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)
	return res
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
	case info.timedOut:
		r.Fail("docker info did not answer within %ds: the Docker daemon is hung, restart Docker Desktop", s.TimeoutSeconds)
		return
	case info.leftover:
		r.Fail("docker info: exited but left a process running, which was stopped: %v", info.err)
		return
	case info.err != nil && !r.Busy(s.Running, s.Backend):
		r.Skip("docker images: Docker Desktop is not running")
		return
	case info.err != nil:
		r.Fail("docker info: %v: %s", info.err, info.output())
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
	for _, line := range strings.Split(pruned.stdout, "\n") {
		if strings.HasPrefix(line, "Total reclaimed space") {
			fmt.Fprintln(r.Out, line)
		}
	}
	switch {
	case pruned.timedOut:
		r.Fail("docker image prune did not finish within %ds", s.PruneTimeoutSeconds)
	case pruned.err != nil:
		r.Fail("docker image prune: %v: %s", pruned.err, pruned.output())
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
	case df.timedOut:
		r.Fail("docker system df did not answer within %ds", s.TimeoutSeconds)
		return "", 0, false
	case df.err != nil:
		r.Fail("docker system df: %v: %s", df.err, df.output())
		return "", 0, false
	}
	for _, line := range strings.Split(df.stdout, "\n") {
		kind, size, found := strings.Cut(line, "\t")
		if !found || kind != "Images" {
			continue
		}
		size, _, _ = strings.Cut(size, " (")
		if kb, ok := sizeKB(size); ok {
			return size, kb, true
		}
	}
	r.Fail("docker system df: cannot read the reclaimable size from: %s", df.output())
	return "", 0, false
}

func sizeKB(size string) (int64, bool) {
	match := humanSize.FindStringSubmatch(size)
	if match == nil {
		return 0, false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return int64(number * bytesPerUnit[match[2]] / 1024), true
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
