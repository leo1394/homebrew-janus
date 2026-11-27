package cli

import (
    "bytes"
    "strings"
    "testing"
    "time"
)
func TestAgentLoadingStopsBeforeRemoteOutput(t *testing.T){
    var out bytes.Buffer
    stop:=startAgentLoading(&out)
    time.Sleep(750*time.Millisecond)
    stop();stop()
    loading:=out.String()
    if !strings.Contains(loading,"⠋ LINKING TO YOUR REMOTE A2X BOT ...")||!strings.Contains(loading,"⠙ LINKING TO YOUR REMOTE A2X BOT .")||!strings.HasSuffix(loading,"\r\x1b[2K"){t.Fatal("missing animation or cleanup",loading)}
    out.WriteString("REMOTE_TUI")
    time.Sleep(550*time.Millisecond)
    if out.String()!=loading+"REMOTE_TUI"{t.Fatal("spinner wrote after handoff")}
}
