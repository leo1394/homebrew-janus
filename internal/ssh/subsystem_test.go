package ssh

import (
    "context"
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
    p.AgentTransport="subsystem";if _,err:=r.agenuxInstanceArguments(context.Background(),p,"m1");err==nil{t.Fatal("subsystem instance accepted")}
}
