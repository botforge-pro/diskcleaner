package report

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/dustin/go-humanize"
)

type Report struct {
	Out      io.Writer
	Err      io.Writer
	DryRun   bool
	Count    int
	KB       int64
	Failures []string
}

func (r *Report) Fail(format string, args ...any) {
	message := strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", "; ")
	fmt.Fprintf(r.Err, "FAILED        %s\n", message)
	r.Failures = append(r.Failures, message)
}

func (r *Report) Skip(format string, args ...any) {
	fmt.Fprintf(r.Err, "SKIPPED       %s\n", fmt.Sprintf(format, args...))
}

func (r *Report) Directory(path string) bool {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		r.Fail("stat %s: %v", path, err)
		return false
	}
	if !info.IsDir() {
		r.Fail("%s: not a directory", path)
		return false
	}
	return true
}

func (r *Report) Busy(running func(pattern string) (bool, error), pattern string) bool {
	busy, err := running(pattern)
	if err != nil {
		r.Fail("pgrep -f '%s': %v", pattern, err)
		return true
	}
	return busy
}

func (r *Report) Remove(path string) {
	sizeKB, err := DiskUsageKB(path)
	if err != nil {
		r.Fail("du %s: %v", path, err)
	}
	if r.DryRun {
		fmt.Fprintf(r.Out, "WOULD REMOVE  %s  %s\n", FormatKB(sizeKB), path)
		r.Count++
		r.KB += sizeKB
		return
	}
	fmt.Fprintf(r.Out, "REMOVE        %s  %s\n", FormatKB(sizeKB), path)
	if err := os.RemoveAll(path); err != nil {
		r.Fail("rm %s: %v", path, err)
		r.countFreed(path, sizeKB, "a failed rm")
		return
	}
	r.Count++
	r.KB += sizeKB
}

func (r *Report) RunCleanup(dir string, name string, args ...string) {
	command := strings.Join(append([]string{name}, args...), " ")
	sizeKB, err := DiskUsageKB(dir)
	if err != nil {
		r.Fail("du %s: %v", dir, err)
	}
	if r.DryRun {
		fmt.Fprintf(r.Out, "WOULD RUN     %s  (%s in %s)\n", command, FormatKB(sizeKB), dir)
		r.Count++
		r.KB += sizeKB
		return
	}
	fmt.Fprintf(r.Out, "RUN           %s  (%s in %s)\n", command, FormatKB(sizeKB), dir)
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		r.Fail("%s: %v: %s", command, err, strings.TrimSpace(string(output)))
	} else {
		r.Count++
	}
	r.countFreed(dir, sizeKB, command)
}

func (r *Report) countFreed(path string, sizeKB int64, after string) {
	_, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		r.KB += sizeKB
		return
	}
	if err != nil {
		r.Fail("stat %s after %s, its freed space is not counted: %v", path, after, err)
		return
	}
	leftKB, err := DiskUsageKB(path)
	if err != nil {
		r.Fail("du %s after %s, its freed space is not counted: %v", path, after, err)
		return
	}
	if leftKB < sizeKB {
		r.KB += sizeKB - leftKB
	}
}

func DiskUsageKB(path string) (int64, error) {
	var blocks int64
	var walkErrors []error
	err := filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			walkErrors = append(walkErrors, err)
			if entry == nil {
				return nil
			}
		}
		info, infoErr := os.Lstat(current)
		if infoErr != nil {
			walkErrors = append(walkErrors, infoErr)
			return nil
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			blocks += stat.Blocks
		}
		return nil
	})
	if err != nil {
		walkErrors = append(walkErrors, err)
	}
	return (blocks*512 + 1023) / 1024, errors.Join(walkErrors...)
}

func FormatKB(kb int64) string {
	return humanize.IBytes(uint64(kb) * 1024)
}

func (r *Report) WriteSummary(w io.Writer) error {
	if _, err := fmt.Fprintf(w, "count %d\nkb %d\n", r.Count, r.KB); err != nil {
		return err
	}
	for _, failure := range r.Failures {
		if _, err := fmt.Fprintf(w, "failure %s\n", failure); err != nil {
			return err
		}
	}
	return nil
}
