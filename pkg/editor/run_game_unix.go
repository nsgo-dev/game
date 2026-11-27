//go:build (darwin || linux || freebsd || openbsd || netbsd) && !android && !ios

package editor

import (
	"errors"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/nsgo-dev/game/internal/gotoolchain"
)

const canRunGameDesktop = true

type unixGameProcess struct {
	cmd      *exec.Cmd
	pgid     int
	waitDone chan struct{}
	waitErr  error
	stopOnce sync.Once
}

func startGameProcess(spec RunGameSpec, stdout, stderr io.Writer) (GameProcess, error) {
	bin := gotoolchain.BinaryForEnv(spec.Name, spec.Env)
	cmd := exec.Command(bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Place the child in its own process group so Stop can terminate both it and every process it spawned.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &unixGameProcess{
		cmd:      cmd,
		pgid:     cmd.Process.Pid,
		waitDone: make(chan struct{}),
	}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.waitDone)
	}()
	return p, nil
}

func (p *unixGameProcess) Wait() error {
	<-p.waitDone
	return p.waitErr
}

func (p *unixGameProcess) Stop() error {
	p.stopOnce.Do(func() {
		// Ask the entire process group to exit gracefully first.
		_ = syscall.Kill(-p.pgid, syscall.SIGTERM)
		if !p.waitProcessGroupEmpty(500 * time.Millisecond) {
			// Escalate to SIGKILL on the entire process group so no descendant survives.
			_ = syscall.Kill(-p.pgid, syscall.SIGKILL)
			_ = p.waitProcessGroupEmpty(2 * time.Second)
		}
	})
	<-p.waitDone
	return nil
}

func (p *unixGameProcess) waitProcessGroupEmpty(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		waitExited := false
		select {
		case <-p.waitDone:
			waitExited = true
		default:
		}
		if waitExited && processGroupEmpty(p.pgid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func processGroupEmpty(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return errors.Is(err, syscall.ESRCH)
}
