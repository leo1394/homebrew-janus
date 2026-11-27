//go:build !windows

package selector

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInputSourceGuardRestoresOnExit(t *testing.T) {
	for _, reason := range []string{"confirm", "cancel", "error"} {
		t.Run(reason, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "restored")
			func() {
				stop := startInputSourceGuard(exec.Command("/bin/sh", "-c", "printf 'ready\\n'; cat >/dev/null; touch \"$1\"", "guard", marker))
				defer stop()
				defer stop()
			}()
			if _, err := os.Stat(marker); err != nil { t.Fatal("guard did not observe EOF and restore:", err) }
		})
	}
}

func TestUnavailableInputSourceGuardIsOptional(t *testing.T) {
	startInputSourceGuard(exec.Command("/nonexistent/janus-helper"))()
	startInputSourceGuard(exec.Command("/bin/sh", "-c", "exit 0"))()
}
