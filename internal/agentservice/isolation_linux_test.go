package agentservice

import (
    "bufio"
    "archive/tar"
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "os"
    "os/exec"
    "os/signal"
    "path/filepath"
    "strings"
    "syscall"
    "testing"
    "time"
)
type osFixtureRemote struct{done chan struct{}}
func(r *osFixtureRemote)Done()<-chan struct{}{return r.done}
func(r *osFixtureRemote)Call(_ context.Context,method string,params,result any)error{
    if method=="delegated.call"{data,_:=json.Marshal(params);var q Request;json.Unmarshal(data,&q);if q.GrantID!="grant-fixture"||q.Credential!="test-only-delegated-capability"{return errors.New("invalid fixture credential")}}
    return json.Unmarshal([]byte(`{"state":"ready"}`),result)
}
func TestUIDIsolationHelper(t *testing.T){
    mode:=os.Getenv("JANUS_UID_HELPER");if mode==""{return};base:=os.Getenv("JANUS_UID_BASE")
    owner:=filepath.Join(base,"owner");public:=filepath.Join(base,"ipc")
    if mode=="owner"{
        if os.Getuid()!=1001{t.Fatal("owner must run under UID1001")}
        if err:=os.WriteFile(filepath.Join(owner,"owner-private"),[]byte("test-only-owner-credential"),0600);err!=nil{t.Fatal(err)}
        master,err:=net.Listen("unix",filepath.Join(owner,"master.sock"));if err!=nil{t.Fatal(err)};defer master.Close();os.Chmod(filepath.Join(owner,"master.sock"),0600)
        ctx,stop:=signal.NotifyContext(context.Background(),syscall.SIGTERM);defer stop()
        err=ServeIsolated(ctx,owner,"instance-fixture",&osFixtureRemote{done:make(chan struct{})},ServeOptions{DelegateDirectory:public,DelegateUIDs:[]uint32{1002}},func(){fmt.Println("READY")})
        if err!=nil{t.Fatal(err)};return
    }
    ctx,cancel:=context.WithTimeout(context.Background(),4*time.Second);defer cancel()
    pinned:=uint32(1001);credential:=Credential{LocalOwnerUID:&pinned,GrantID:"grant-fixture",Credential:"test-only-delegated-capability",InstanceID:"instance-fixture"}
    if mode=="delegate"||mode=="container"{
        if os.Getuid()!=1002{t.Fatal("delegate must run under UID1002")}
        loaded,err:=ReadCredential(filepath.Join(base,"delegate","grant.json"));if err!=nil{t.Fatal(err)}
        response,err:=CallCredential(ctx,public,loaded,"agent.status",[]byte(`{}`));if err!=nil||!response.OK{t.Fatal("authorized delegate failed",response,err)}
        loaded.Credential="wrong";response,err=CallCredential(ctx,public,loaded,"agent.status",[]byte(`{}`));if err==nil&&response.OK{t.Fatal("wrong grant accepted")}
        response,err=CallCredential(ctx,public,credential,"terminal.open",[]byte(`{}`));if err==nil&&response.OK{t.Fatal("delegated terminal accepted")}
        // Exercise the raw wire too: privilege denial cannot rely on CLI checks.
        data,_:=json.Marshal(Request{InstanceID:credential.InstanceID,GrantID:credential.GrantID,Credential:credential.Credential,Method:"grant.create",Params:json.RawMessage(`{}`)})
        conn,err:=net.Dial("unix",filepath.Join(public,"delegated.sock"));if err!=nil{t.Fatal(err)};conn.SetDeadline(time.Now().Add(time.Second));fmt.Fprintf(conn,"POST /call HTTP/1.1\r\nHost: janus\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",len(data),data);reply,_:=io.ReadAll(conn);conn.Close();if bytes.Contains(reply,[]byte(`"ok":true`)){t.Fatal("raw grant creation bypass")}
        if _,err:=os.ReadFile(filepath.Join(owner,"owner-private"));!os.IsPermission(err)&&!(mode=="container"&&os.IsNotExist(err)){t.Fatal("owner credential accessible",err)}
        for _,name:=range []string{"owner.sock","master.sock"}{conn,err:=net.Dial("unix",filepath.Join(owner,name));if conn!=nil{conn.Close()};if err==nil{t.Fatal("private socket accessible",name)}}
        if err:=os.Remove(filepath.Join(public,"delegated.sock"));!os.IsPermission(err)&&!errors.Is(err,syscall.EROFS){t.Fatal("delegate could replace IPC socket",err)}
        if _,err:=os.Stat("/janus");mode=="container"&&err==nil{
            command:=exec.Command(os.Getenv("JANUS_UID_CLI"),"agent","delegated","--directory",public,"--credential-file",filepath.Join(base,"delegate","grant.json"),"--method","agent.status");command.Stdin=strings.NewReader(`{}`);command.Env=append(os.Environ(),"HOME=/tmp")
            out,err:=command.CombinedOutput();if err!=nil||!bytes.Contains(out,[]byte(`"ok":true`)){t.Fatal("delegate CLI failed",string(out),err)};if bytes.Contains(out,[]byte(credential.Credential)){t.Fatal("CLI logged credential")}
        }
        wrongUID:=uint32(1003);credential.LocalOwnerUID=&wrongUID;if _,err:=CallCredential(ctx,public,credential,"agent.status",[]byte(`{}`));err==nil{t.Fatal("server owner pin bypass")}
        return
    }
    if mode=="other"{
        if os.Getuid()!=1003{t.Fatal("unauthorized peer must run under UID1003")}
        // Even possession of the same test capability is insufficient without UID authorization.
        response,err:=CallCredential(ctx,public,credential,"agent.status",[]byte(`{}`));if err==nil&&response.OK{t.Fatal("unlisted UID accepted")}
        if _,err:=os.ReadFile(filepath.Join(base,"delegate","grant.json"));!os.IsPermission(err){t.Fatal("another UID read delegate grant",err)}
        return
    }
    t.Fatal("unknown helper mode")
}
func TestUIDIsolationOS(t *testing.T){
    if os.Getenv("JANUS_UID_OS")!="1"{t.Skip("opt-in root coordinator in isolated Linux VM")};if os.Getuid()!=0{t.Fatal("requires root only to launch isolated UID fixtures")}
    base,err:=os.MkdirTemp("/tmp","janus-uid-");if err!=nil{t.Fatal(err)};defer os.RemoveAll(base);os.Chmod(base,0755)
    for _,dir:=range []struct{name string;uid int;mode os.FileMode}{{"owner",1001,0700},{"ipc",1001,0755},{"delegate",1002,0700}}{path:=filepath.Join(base,dir.name);if err:=os.Mkdir(path,dir.mode);err!=nil{t.Fatal(err)};if err:=os.Chown(path,dir.uid,dir.uid);err!=nil{t.Fatal(err)}}
    executable,_:=os.Executable();data,err:=os.ReadFile(executable);if err!=nil{t.Fatal(err)};probe:=filepath.Join(base,"probe");if err:=os.WriteFile(probe,data,0755);err!=nil{t.Fatal(err)}
    pinned:=uint32(1001);data,_=json.Marshal(Credential{LocalOwnerUID:&pinned,GrantID:"grant-fixture",Credential:"test-only-delegated-capability",InstanceID:"instance-fixture"});grant:=filepath.Join(base,"delegate","grant.json");os.WriteFile(grant,data,0600);os.Chown(grant,1002,1002)
    child:=func(mode string,uid uint32)*exec.Cmd{cmd:=exec.Command(probe,"-test.run=^TestUIDIsolationHelper$","-test.v");cmd.Env=append(os.Environ(),"JANUS_UID_HELPER="+mode,"JANUS_UID_BASE="+base);cmd.SysProcAttr=&syscall.SysProcAttr{Credential:&syscall.Credential{Uid:uid,Gid:uid,Groups:[]uint32{}}};return cmd}
    owner:=child("owner",1001);stdout,err:=owner.StdoutPipe();if err!=nil{t.Fatal(err)};var stderr bytes.Buffer;owner.Stderr=&stderr;if err:=owner.Start();err!=nil{t.Fatal(err)}
    defer func(){owner.Process.Signal(syscall.SIGTERM);if err:=owner.Wait();err!=nil{t.Error("owner shutdown",err,stderr.String())}}()
    ready:=make(chan bool,1);go func(){scanner:=bufio.NewScanner(stdout);for scanner.Scan(){if strings.TrimSpace(scanner.Text())=="READY"{ready<-true;return}};ready<-false}()
    select{case ok:=<-ready:if !ok{t.Fatal("owner did not become ready")};case <-time.After(5*time.Second):t.Fatal("owner readiness timeout")}
    for _,probe:=range []struct{mode string;uid uint32}{{"delegate",1002},{"other",1003}}{out,err:=child(probe.mode,probe.uid).CombinedOutput();if err!=nil{t.Fatal(probe.mode,string(out),err)}}
    if os.Getenv("JANUS_UID_CONTAINER")=="1"{
        var archive bytes.Buffer;tarWriter:=tar.NewWriter(&archive)
        dockerfile:=[]byte("FROM scratch\nCOPY probe /probe\nENTRYPOINT [\"/probe\",\"-test.run=^TestUIDIsolationHelper$\",\"-test.v\"]\n")
        binary,err:=os.ReadFile(probe);if err!=nil{t.Fatal(err)}
        files:=[]struct{name string;data []byte;mode int64}{{"Dockerfile",dockerfile,0600},{"probe",binary,0755}}
        if cli:=os.Getenv("JANUS_UID_CLI");cli!=""{data,err:=os.ReadFile(cli);if err!=nil{t.Fatal(err)};files[0].data=append(dockerfile,[]byte("COPY janus /janus\n")...);files=append(files,struct{name string;data []byte;mode int64}{"janus",data,0755})}
        for _,file:=range files{if err:=tarWriter.WriteHeader(&tar.Header{Name:file.name,Mode:file.mode,Size:int64(len(file.data))});err!=nil{t.Fatal(err)};if _,err:=tarWriter.Write(file.data);err!=nil{t.Fatal(err)}};tarWriter.Close()
        build:=exec.Command("docker","build","-t","janus-uid-probe:local","-");build.Stdin=&archive;if out,err:=build.CombinedOutput();err!=nil{t.Fatal("build isolated probe",string(out),err)}
        run:=exec.Command("docker","run","--rm","--user","1002:1002","--network","none","--cap-drop","ALL","--security-opt","no-new-privileges","--read-only","--pids-limit","64","--mount","type=bind,src="+filepath.Join(base,"ipc")+",dst=/fixture/ipc,readonly","--mount","type=bind,src="+grant+",dst=/fixture/delegate/grant.json,readonly","--env","JANUS_UID_HELPER=container","--env","JANUS_UID_BASE=/fixture","--env","JANUS_UID_CLI=/janus","janus-uid-probe:local")
        if out,err:=run.CombinedOutput();err!=nil{t.Fatal("container delegate",string(out),err)}
        t.Log("scratch container UID1002 with only delegated socket and own grant, no network/capabilities, passed")
    }
    t.Log("UID1001 owner: private sockets/credentials inaccessible; UID1002 delegated call allowed; UID1003 rejected even with fixture capability")
}
