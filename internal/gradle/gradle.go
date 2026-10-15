package gradle

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/botforge-pro/diskcleaner/internal/process"
	"github.com/botforge-pro/diskcleaner/internal/report"
	"github.com/botforge-pro/diskcleaner/internal/settings"
)

const (
	quietCheck   = 2 * time.Second
	stopDeadline = 10 * time.Second
	stopPoll     = time.Second
)

type Step struct {
	Caches    string
	Daemons   string
	Clients   string
	Running   func(pattern string) (bool, error)
	Pids      func(pattern string) ([]string, error)
	CPUTimes  func(pids []string) (string, error)
	Terminate func(pattern string) error
	Sleep     func(time.Duration)
}

func Default(home string) Step {
	return Step{
		Caches:    filepath.Join(settings.Env("GRADLE_USER_HOME", filepath.Join(home, ".gradle")), "caches"),
		Daemons:   settings.Env("DISKCLEANER_GRADLE_DAEMONS", `org\.gradle\.launcher\.daemon\.bootstrap\.GradleDaemon|org\.jetbrains\.kotlin\.daemon\.KotlinCompileDaemon`),
		Clients:   settings.Env("DISKCLEANER_GRADLE_CLIENTS", `org\.gradle\.wrapper\.GradleWrapperMain|org\.gradle\.launcher\.GradleMain|/Contents/MacOS/studio|/Contents/MacOS/idea`),
		Running:   process.Running,
		Pids:      process.Pids,
		CPUTimes:  process.CPUTimes,
		Terminate: process.Terminate,
		Sleep:     time.Sleep,
	}
}

func (s Step) Run(r *report.Report) {
	if !r.Directory(s.Caches) {
		return
	}
	fmt.Fprintf(r.Out, "GRADLE   %s\n", s.Caches)
	defer fmt.Fprintln(r.Out)

	if r.Busy(s.Running, s.Clients) {
		r.Skip("%s: a Gradle build or Android Studio / IntelliJ IDEA is running", s.Caches)
		return
	}
	daemons, ok := s.daemonCPU(r)
	if !ok {
		return
	}
	if daemons != "" {
		s.Sleep(quietCheck)
		again, ok := s.daemonCPU(r)
		if !ok {
			return
		}
		if again != daemons {
			r.Skip("%s: a Gradle or Kotlin daemon is using the CPU", s.Caches)
			return
		}
		if !s.stopDaemons(r) {
			return
		}
	}
	r.Remove(s.Caches)
}

func (s Step) daemonCPU(r *report.Report) (string, bool) {
	pids, err := s.Pids(s.Daemons)
	if err != nil {
		r.Fail("pgrep -f '%s': %v", s.Daemons, err)
		return "", false
	}
	times, err := s.CPUTimes(pids)
	if err != nil {
		r.Fail("ps of the Gradle and Kotlin daemons: %v", err)
		return "", false
	}
	return times, true
}

func (s Step) stopDaemons(r *report.Report) bool {
	if r.DryRun {
		fmt.Fprintln(r.Out, "WOULD STOP    idle Gradle and Kotlin daemons")
		return true
	}
	fmt.Fprintln(r.Out, "STOP          idle Gradle and Kotlin daemons")
	if err := s.Terminate(s.Daemons); err != nil {
		r.Fail("pkill -TERM -f '%s': %v", s.Daemons, err)
		return false
	}
	for waited := time.Duration(0); ; waited += stopPoll {
		pids, err := s.Pids(s.Daemons)
		if err != nil {
			r.Fail("pgrep -f '%s': %v", s.Daemons, err)
			return false
		}
		if len(pids) == 0 {
			return true
		}
		if waited >= stopDeadline {
			r.Fail("%s: Gradle or Kotlin daemons did not stop within %s", s.Caches, stopDeadline)
			return false
		}
		s.Sleep(stopPoll)
	}
}
