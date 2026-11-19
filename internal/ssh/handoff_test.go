package ssh

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
)

func TestSessionProcessHandoff(t *testing.T){
    if os.Getenv("JANUS_TEST_HANDOFF")=="1"{
        fmt.Fprintf(os.Stdout,"before=%d\n",os.Getpid())
        r:=Runtime{Binary:os.Getenv("JANUS_TEST_HANDOFF_BINARY"),In:os.Stdin,Out:os.Stdout,Err:os.Stderr}
        if err:=r.run(context.Background(),[]string{"session-argument"},nil,true);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(90)}
        os.Exit(91)
    }
    binary:=filepath.Join(t.TempDir(),"ssh-fixture")
    script:="#!/bin/sh\nprintf 'after=%s arg=%s\\n' \"$$\" \"$1\"\nIFS= read -r line\nprintf 'input=%s\\n' \"$line\"\nexit 7\n"
    if err:=os.WriteFile(binary,[]byte(script),0700);err!=nil{t.Fatal(err)}
    cmd:=exec.Command(os.Args[0],"-test.run=^TestSessionProcessHandoff$")
    cmd.Env=append(os.Environ(),"JANUS_TEST_HANDOFF=1","JANUS_TEST_HANDOFF_BINARY="+binary,"JANUS_TRACE=1")
    cmd.Stdin=strings.NewReader("unchanged input\n")
    out,err:=cmd.CombinedOutput()
    exit,ok:=err.(*exec.ExitError);if !ok||exit.ExitCode()!=7{t.Fatalf("SSH exit status not preserved: %s %v",out,err)}
    lines:=strings.Split(strings.TrimSpace(string(out)),"\n")
    if len(lines)!=3{t.Fatalf("unexpected output or session tracing: %s",out)}
    pid:=strings.TrimPrefix(lines[0],"before=")
    if lines[1]!="after="+pid+" arg=session-argument"||lines[2]!="input=unchanged input"{t.Fatalf("process, arguments or input not handed over: %s",out)}
}
