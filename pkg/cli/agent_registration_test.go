package cli

import (
    "bytes"
    "io"
    "os"
    "path/filepath"
    "reflect"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
func TestSpawnRegistersOnlyAuthenticatedIdentity(t *testing.T){
    for _,tc:=range []struct{name string;denied,legacy bool;expected string}{{"detected opencode",false,false,""},{"MFA denied",true,false,""},{"type mismatch",false,false,"pi"},{"unknown legacy identity",false,true,""}}{t.Run(tc.name,func(t *testing.T){
        s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{mfa:true,denyMFA:tc.denied,infoType:"opencode",taskOnly:true,legacyInfo:tc.legacy}
        run:=func(){t.Helper();cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};var output bytes.Buffer;cmd.SetOut(&output);cmd.SetErr(io.Discard);cmd.SetIn(strings.NewReader("/exit\n"));args:=[]string{"spawn","agent","alice@example.com"};if tc.expected!=""{args=append(args,"--type",tc.expected)};cmd.SetArgs(args);err=cmd.Execute();failed:=tc.denied||tc.legacy||tc.expected!="";if (err!=nil)!=failed{t.Fatal(err)};if !failed&&!strings.Contains(output.String(),"machine tasks"){t.Fatal(output.String())}}
        run();rows,err:=s.List();if err!=nil{t.Fatal(err)}
        if tc.denied||tc.legacy||tc.expected!=""{if len(rows)!=0{t.Fatal("failed auth/type wrote registry",rows)};if _,err:=os.Stat(filepath.Join(s.Root,"devices.json"));!os.IsNotExist(err){t.Fatal("registry file created on failure",err)};return}
        if len(rows)!=1||rows[0].Type!="opencode"||rows[0].InstanceID!="fixture-instance"||rows[0].LastSession!=""{t.Fatal(rows)};id:=rows[0].ID
        if r.prompts!=1{t.Fatal("duplicate MFA",r.prompts)};run();rows,_=s.List();if len(rows)!=1||rows[0].ID!=id{t.Fatal("spawn created fake Pi duplicate",rows)}
        r.mu.Lock();methods:=strings.Join(r.methods,",");r.mu.Unlock();if strings.Contains(methods,"session."){t.Fatal("machine instance invoked sessions",methods)}
    })}
}
func TestConnectIdentityMismatchDoesNotWrite(t *testing.T){
    for _,tc:=range []struct{name,kind,instance string}{{"type","opencode","saved"},{"instance","pi","different"}}{t.Run(tc.name,func(t *testing.T){
        s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.AddAgent("alice","example.com",7263,"subsystem");if err!=nil{t.Fatal(err)};if err:=s.BindAgentInstance(p.ID,"saved");err!=nil{t.Fatal(err)}
        before,_:=os.ReadFile(filepath.Join(s.Root,"devices.json"));r:=&agentFake{infoType:tc.kind,infoInstance:tc.instance}
        cmd,_:=New(Options{Store:s,Runtime:r});cmd.SetArgs([]string{"connect",p.ID});cmd.SetOut(io.Discard);cmd.SetErr(io.Discard);if cmd.Execute()==nil{t.Fatal("mismatched identity connected")};after,_:=os.ReadFile(filepath.Join(s.Root,"devices.json"));if !bytes.Equal(before,after){t.Fatal("mismatch mutated registry")}
    })}
}
func TestLegacyAgentCompatibility(t *testing.T){
    for _,missingInfo:=range []bool{false,true}{
        s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,_:=s.AddAgent("alice","example.com",7263,"stdio");p.Platform="ai";p.Type="agent";p.LastSession="s1";s.Save([]janus.Profile{p})
        kind:="opencode";if missingInfo{kind="pi"};r:=&agentFake{legacyInfo:missingInfo,infoType:kind,taskOnly:!missingInfo}
        cmd,_:=New(Options{Store:s,Runtime:r});cmd.SetArgs([]string{"connect",p.ID});cmd.SetIn(strings.NewReader("/exit\n"));cmd.SetOut(io.Discard);cmd.SetErr(io.Discard);if err:=cmd.Execute();err!=nil{t.Fatal(err)}
        got,_:=s.Get(p.ID);if got.ID!=p.ID||got.LastSession!=p.LastSession||store.Target(got)!=store.Target(p){t.Fatal(got)}
        if missingInfo{if !reflect.DeepEqual(got,p){t.Fatal("legacy identity was fabricated",got)}}else if got.Type!="opencode"||got.InstanceID!="fixture-instance"{t.Fatal(got)}
    }
}
