package cli

import (
    "bytes"
    "context"
    "path/filepath"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

type credentialRuntime struct{janus.SSHRuntime; recovered string}
func(r *credentialRuntime)RecoverCredential(_ context.Context,p janus.Profile)error{r.recovered=p.ID;return nil}
func TestCredentialRecoveryCommand(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)}
    for _,args:=range [][]string{{"credential","--recover",p.Name},{"credential",p.Name},{"credential","--recover=false",p.Name},{"credential","--recover"},{"recover-credential",p.Name}}{
        r:=&credentialRuntime{};cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};var out bytes.Buffer;cmd.SetOut(&out);cmd.SetErr(&out);cmd.SetArgs(args);err=cmd.Execute()
        valid:=len(args)==3&&args[1]=="--recover"
        if valid{if err!=nil||r.recovered!=p.ID{t.Fatal(args,err,r.recovered)}}else if err==nil||r.recovered!=""{t.Fatal("invalid recovery invoked",args,err)}
    }
    out,err:=execute(t,s,"__complete","credential","--recover","ali");if err!=nil||!strings.Contains(out,p.Name){t.Fatal(out,err)}
    out,err=execute(t,s,"credential","--help");if err!=nil||!strings.Contains(out,"--recover"){t.Fatal(out,err)}
}
