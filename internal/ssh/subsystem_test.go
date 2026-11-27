package ssh

import (
    "context"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
)
func TestAgenuxStdioLauncher(t *testing.T){
    for _,tc:=range []struct{name string;pathBinary,localBinary bool;want string}{
        {"path takes precedence",true,true,"path:stdio"},
        {"restricted path falls back",false,true,"local:stdio"},
        {"missing executable fails",false,false,""},
    }{t.Run(tc.name,func(t *testing.T){
        root:=t.TempDir();home:=filepath.Join(root,"home with spaces 'quoted'");bin:=filepath.Join(root,"path with spaces")
        local:=filepath.Join(home,".local","bin")
        for _,dir:=range []string{bin,local}{if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}}
        for _,fixture:=range []struct{enabled bool;dir,label string}{{tc.pathBinary,bin,"path"},{tc.localBinary,local,"local"}}{
            if fixture.enabled {script:="#!/bin/sh\nprintf '"+fixture.label+":%s\\n' \"$*\"\n";if err:=os.WriteFile(filepath.Join(fixture.dir,"agenux"),[]byte(script),0700);err!=nil{t.Fatal(err)}}
        }
        command:=exec.Command("/bin/sh","-c",agenuxStdioLauncher)
        command.Env=[]string{"PATH="+bin,"HOME="+home}
        out,err:=command.Output()
        if tc.want==""{if err==nil{t.Fatal("missing agent succeeded")};return}
        if err!=nil||strings.TrimSpace(string(out))!=tc.want{t.Fatal(string(out),err)}
    })}
}
func TestBorrowedMasterNeverAuthenticates(t *testing.T){
    r,p:=fixture(t);socket(t,r,p)
    r.AgentControlSocket=r.path(p);r.CredentialNamespace=strings.Repeat("a",64)
    r.Run=func(context.Context,[]string,[]string,bool)error{t.Fatal("borrowed master attempted authentication or control");return nil}
    p.AgentTransport="stdio"
    args,err:=r.agenuxArguments(context.Background(),p);if err!=nil{t.Fatal(err)}
    joined:=strings.Join(args," ")
    for _,required:=range []string{r.AgentControlSocket,"BatchMode=yes","ControlMaster=no","ProxyCommand=/usr/bin/false",agenuxStdioLauncher}{if !strings.Contains(joined,required){t.Fatal(args)}}
    r.CredentialNamespace="";if _,err:=r.agenuxArguments(context.Background(),p);err==nil{t.Fatal("production borrowed socket accepted")}
    regular:=filepath.Join(t.TempDir(),"file");os.WriteFile(regular,[]byte("not socket"),0600)
    if err:=validateAgentSocket(regular);err==nil{t.Fatal("regular file accepted")}
}

func TestNamedAgenuxInstanceArguments(t *testing.T){
    r,p:=fixture(t);socket(t,r,p);r.AgentControlSocket=r.path(p);r.CredentialNamespace=strings.Repeat("a",64)
    r.Run=func(context.Context,[]string,[]string,bool)error{t.Fatal("named instance reauthenticated");return nil};p.AgentTransport="stdio"
    args,err:=r.agenuxInstanceArguments(context.Background(),p,"m1-test");if err!=nil{t.Fatal(err)}
    if args[len(args)-1]!=`exec "$HOME/.local/bin/agenux-instance" m1-test stdio`{t.Fatal(args)}
    p.InstanceName="m1-test";savedArgs,err:=r.agenuxArguments(context.Background(),p);if err!=nil||strings.Join(savedArgs," ")!=strings.Join(args," "){t.Fatal("saved instance launcher",savedArgs,err)};p.InstanceName=""
    for _,name:=range []string{"","A","x;id","x y","../x","$(id)",strings.Repeat("a",33)}{if _,err:=r.agenuxInstanceArguments(context.Background(),p,name);err==nil{t.Fatal("unsafe instance accepted",name)}}
    p.AgentTransport="subsystem"
    for _,name:=range []string{"m1","default"}{
        args,err:=r.agenuxInstanceArguments(context.Background(),p,name);want:="agenux-"+name;if name=="default"{want="agenux"}
        if err!=nil||args[len(args)-1]!=want||!strings.Contains(strings.Join(args," "),"-s "){t.Fatal(args,err)}
        p.InstanceName=name;saved,err:=r.agenuxArguments(context.Background(),p);if err!=nil||saved[len(saved)-1]!=want{t.Fatal(saved,err)};p.InstanceName=""
    }
}

func TestDiscoveryAndNamedInstanceReuseTerminalMaster(t *testing.T){
    r,terminal:=fixture(t);starts:=0
    r.Run=func(_ context.Context,args,env []string,tty bool)error{
        if strings.Contains(strings.Join(args," "),"-M -N -f"){starts++;listener,err:=net.Listen("unix",args[3]);if err!=nil{t.Fatal(err)};t.Cleanup(func(){listener.Close()})}
        return nil
    }
    probe,err:=r.Store.PrepareAgentDiscovery(terminal.User,terminal.Host,terminal.Port,"subsystem");if err!=nil{t.Fatal(err)}
    discovery,err:=r.agenuxRegistrationArguments(context.Background(),probe,true);if err!=nil{t.Fatal(err)}
    named,pending,err:=r.Store.PrepareAgentInstance(terminal.User,terminal.Host,terminal.Port,"subsystem","","opencode-main");if err!=nil{t.Fatal(err)}
    connected,err:=r.agenuxRegistrationArguments(context.Background(),named,pending);if err!=nil{t.Fatal(err)}
    if r.path(named)!=r.path(terminal){t.Fatal("agent status path differs from reused master")}
    if starts!=1||discovery[3]!=r.path(terminal)||connected[3]!=r.path(terminal){t.Fatalf("master starts=%d discovery=%v instance=%v",starts,discovery,connected)}
    r.Run=func(_ context.Context,args,env []string,tty bool)error{if strings.Contains(strings.Join(args," "),"-O stop"){t.Fatal("pending cancellation stopped shared terminal")};return nil}
    if err:=r.DiscardPendingAgent(context.Background(),named);err!=nil{t.Fatal(err)}
}

func TestAgentTransportKeySurvivesRegistrationAndSeparatesCredentials(t *testing.T){
    r,terminal:=fixture(t)
    p,err:=r.Store.PrepareAgentDiscovery("bob",terminal.Host,22,"subsystem");if err!=nil{t.Fatal(err)}
    q,pending,err:=r.Store.PrepareAgentInstance("bob",terminal.Host,22,"subsystem","","opencode-main");if err!=nil||!pending{t.Fatal(err)}
    path:=r.path(p);if r.path(q)!=path{t.Fatal("discovery and instance transport differ")}
    saved,err:=r.Store.RegisterAuthenticatedAgent(q,true,"instance-id","opencode","");if err!=nil{t.Fatal(err)}
    if r.path(saved)!=path{t.Fatal("registration changed master path")}
    other:=saved;other.CredentialSlot="replacement";if r.path(other)==path{t.Fatal("different credential reused master")}
    other=saved;other.User="another";if r.path(other)==path{t.Fatal("different user reused master")}
    other=saved;other.Port=23;if r.path(other)==path{t.Fatal("different port reused master")}
}
