//go:build windows

package editor

import (
	"io"
	"os/exec"
	"strconv"
	"sync"
)

const canRunGameDesktop = true

type windowsGameProcess struct {
	cmd      *exec.Cmd
	pid      int
	waitDone chan struct{}
	waitErr  error
	stopOnce sync.Once
}

func startGameProcess(spec RunGameSpec, stdout, stderr io.Writer) (GameProcess, error) {
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &windowsGameProcess{
		cmd:      cmd,
		pid:      cmd.Process.Pid,
		waitDone: make(chan struct{}),
	}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.waitDone)
	}()
	return p, nil
}

func (p *windowsGameProcess) Wait() error {
	<-p.waitDone
	return p.waitErr
}

func (p *windowsGameProcess) Stop() error {
	p.stopOnce.Do(func() {
		// Terminate the process and its entire child tree on Windows.
		if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.pid)).Run(); err != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	})
	<-p.waitDone
	return nil
}
