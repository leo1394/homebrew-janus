package ssh

import (
    "bufio"
    "bytes"
    "context"
    "fmt"
    "io"
    "net"
    "os"
    "os/exec"
    "os/user"
    "path/filepath"
    "strconv"
    "strings"
    "testing"
    "github.com/leo1394/homebrew-janus/pkg/janus"
    "time"
)

// Opt-in, isolated loopback sshd. Never reads real keys or user SSH config.
func TestRealMuxDisconnectPreservesSession(t *testing.T){
    if os.Getenv("JANUS_TEST_SSHD")!="1"{t.Skip("set JANUS_TEST_SSHD=1 to run the isolated system-OpenSSH integration test")}
    r,p:=fixture(t)
    root:=r.Store.Root
    for _,name:=range []string{"host","client"}{if out,err:=exec.Command("/usr/bin/ssh-keygen","-q","-t","ed25519","-N","","-f",filepath.Join(root,name)).CombinedOutput();err!=nil{t.Fatalf("keygen: %s %v",out,err)}}
    public,err:=os.ReadFile(filepath.Join(root,"client.pub"));if err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(filepath.Join(root,"authorized_keys"),public,0600);err!=nil{t.Fatal(err)}
    account,err:=user.Current();if err!=nil{t.Fatal(err)}
    listener,err:=net.Listen("tcp","127.0.0.1:0");if err!=nil{t.Fatal(err)};port:=listener.Addr().(*net.TCPAddr).Port;listener.Close()
    config:=fmt.Sprintf("ListenAddress 127.0.0.1\nPort %d\nHostKey %s/host\nPidFile %s/sshd.pid\nAuthorizedKeysFile %s/authorized_keys\nStrictModes no\nUsePAM no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\nUseDNS no\nLogLevel ERROR\nAllowUsers %s\n",port,root,root,root,account.Username)
    configPath:=filepath.Join(root,"sshd_config");os.WriteFile(configPath,[]byte(config),0600)
    log,err:=os.Create(filepath.Join(root,"server.log"));if err!=nil{t.Fatal(err)};defer log.Close()
    server:=exec.Command("/usr/sbin/sshd","-D","-e","-f",configPath);server.Stdout=log;server.Stderr=log
    if err:=server.Start();err!=nil{t.Fatal(err)}
    defer func(){server.Process.Kill();server.Wait()}()
    deadline:=time.Now().Add(5*time.Second);listening:=false
    for time.Now().Before(deadline){c,e:=net.DialTimeout("tcp",net.JoinHostPort("127.0.0.1",strconv.Itoa(port)),50*time.Millisecond);if e==nil{c.Close();listening=true;break};time.Sleep(25*time.Millisecond)}
    if !listening{data,_:=os.ReadFile(log.Name());t.Fatalf("fixture sshd did not listen: %s",data)}
    p.Host="127.0.0.1";p.Port=port;p.User=account.Username
    if err:=r.Store.Save([]janus.Profile{p});err!=nil{t.Fatal(err)}
    args:=append(r.base(p),"-M","-N","-f","-i",filepath.Join(root,"client"),"-o","IdentitiesOnly=yes","-o","BatchMode=yes","-o","ControlPersist=30","-o","StrictHostKeyChecking=no","-o","UserKnownHostsFile="+filepath.Join(root,"known_hosts"),p.Host)
    // base's earlier StrictHostKeyChecking=ask takes precedence; preseed the host key.
    hostKey,_:=os.ReadFile(filepath.Join(root,"host.pub"));parts:=strings.Fields(string(hostKey));known:=fmt.Sprintf("[127.0.0.1]:%d %s %s\n",port,parts[0],parts[1]);os.WriteFile(filepath.Join(root,"known_hosts"),[]byte(known),0600)
    ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
    if out,err:=exec.CommandContext(ctx,"/usr/bin/ssh",args...).CombinedOutput();err!=nil{data,_:=os.ReadFile(log.Name());t.Fatalf("fixture master: %s %v; server: %s",out,err,data)}
    defer exec.Command("/usr/bin/ssh",append(r.base(p),"-O","exit",p.Host)...).Run()
    r.Helper="/does/not/exist";r.Err=io.Discard
    reuseStart:=time.Now()
    if err:=r.Ensure(ctx,p);err!=nil{t.Fatal("reuse should not require a helper",err)}
    t.Logf("reused master ready in %s",time.Since(reuseStart))
    input:=strings.NewReader("echo JANUS_READY\nsleep 1\necho JANUS_SURVIVED\nexit\n")
    reader,writer:=io.Pipe();r.In=input;r.Out=writer
    ready:=make(chan struct{},1);var output bytes.Buffer;scanned:=make(chan struct{})
    go func(){defer close(scanned);scanner:=bufio.NewScanner(reader);for scanner.Scan(){line:=scanner.Text();output.WriteString(line+"\n");if line=="JANUS_READY"{select{case ready<-struct{}{}:default:}}}}()
    done:=make(chan error,1);go func(){err:=r.Attach(ctx,p);writer.Close();done<-err}()
    select{case <-ready:case err:=<-done:t.Fatalf("session ended before disconnect: %v",err);case <-ctx.Done():t.Fatal(ctx.Err())}
    if err:=r.Disconnect(ctx,p);err!=nil{t.Fatal(err)}
    if live,err:=r.Connected(ctx,p);err!=nil||live{t.Fatal("stopped master is still reusable",live,err)}
    if err:=<-done;err!=nil{t.Fatal("existing session interrupted",err)};<-scanned
    if !strings.Contains(output.String(),"JANUS_SURVIVED"){t.Fatal(output.String())}
    if err:=r.Attach(ctx,p);err==nil{t.Fatal("new session unexpectedly attached after stop")}
}
