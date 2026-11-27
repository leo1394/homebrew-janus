//go:build !windows

package selector

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

func selectorInputSource() func() {
	if runtime.GOOS != "darwin" || os.Getenv("SSH_CONNECTION") != "" { return func() {} }
	binary, err := os.Executable()
	if err != nil { return func() {} }
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil { return func() {} }
	return startInputSourceGuard(exec.Command(filepath.Join(filepath.Dir(binary), "..", "libexec", "janus-keychain"), "selector-input-source"))
}

// Closing the pipe restores the source even if Janus exits unexpectedly.
func startInputSourceGuard(cmd *exec.Cmd) func() {
	input, err := cmd.StdinPipe()
	if err != nil { return func() {} }
	output, err := cmd.StdoutPipe()
	if err != nil { input.Close(); return func() {} }
	if err = cmd.Start(); err != nil { input.Close(); output.Close(); return func() {} }
	ready := make(chan struct{})
	go func() { _, _ = bufio.NewReader(output).ReadString('\n'); close(ready) }()
	var once sync.Once
	stop := func() { once.Do(func() {
		input.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}) }
	select {
	case <-ready:
	case <-time.After(time.Second): stop()
	}
	return stop
}
