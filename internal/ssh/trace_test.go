package ssh

import (
    "bytes"
    "os"
    "strings"
    "testing"
    "time"
)
func TestTracePreservesFailureAndRemovesRawLog(t *testing.T){
    var output bytes.Buffer
    path,stop,err:=traceSSH(&output);if err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(path,[]byte("debug1: private diagnostic\nConnection to example.invalid closed by remote host.\n"),0600);err!=nil{t.Fatal(err)}
    stop(true)
    if !strings.Contains(output.String(),"closed by remote host")||strings.Contains(output.String(),"private diagnostic"){t.Fatal(output.String())}
    if _,err:=os.Stat(path);!os.IsNotExist(err){t.Fatal("trace file retained",err)}
}
func TestIdleTraceStopsPromptly(t *testing.T){
    _,stop,err:=traceSSH(nil);if err!=nil{t.Fatal(err)}
    start:=time.Now();stop(false)
    if time.Since(start)>time.Second{t.Fatal("idle trace did not stop")}
}

func TestTraceHandshakeMilestonesWithoutRawDetails(t *testing.T){
    t.Setenv("JANUS_TRACE","1")
    captured,err:=os.CreateTemp(t.TempDir(),"milestones");if err!=nil{t.Fatal(err)};defer captured.Close()
    previous:=os.Stderr;os.Stderr=captured;defer func(){os.Stderr=previous}()
    path,stop,err:=traceSSH(nil);if err!=nil{t.Fatal(err)}
    lines:=[]string{"debug1: SSH2_MSG_KEXINIT received","debug1: expecting SSH2_MSG_KEX_ECDH_REPLY","debug1: SSH2_MSG_KEX_ECDH_REPLY received","debug1: SSH2_MSG_NEWKEYS sent","debug3: send packet: type 50","debug1: Authentications that can continue: password","debug1: Next authentication method: password","debug3: send packet: type 50","debug2: we sent a password packet, wait for reply"}
    if err:=os.WriteFile(path,[]byte(strings.Join(lines,"\n")+"\n"),0600);err!=nil{stop(false);t.Fatal(err)}
    stop(false)
    data,err:=os.ReadFile(captured.Name());if err!=nil{t.Fatal(err)};output:=string(data)
    for _,phase:=range []string{"key_exchange_proposal_received","key_exchange_reply_wait","key_exchange_reply_received","client_newkeys_sent","auth_probe_sent","auth_methods_received","password_method_selected","password_packet_sent"}{if strings.Count(output,"phase="+phase+" ")!=1{t.Fatalf("missing/duplicate %s: %s",phase,output)}}
    if strings.Contains(output,"debug")||strings.Contains(output,"Authentications that can continue"){t.Fatal("raw SSH diagnostics leaked",output)}
}
