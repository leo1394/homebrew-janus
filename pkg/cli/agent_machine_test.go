package cli

import (
    "bytes"
    "encoding/json"
    "io"
    "path/filepath"
    "reflect"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/store"
)
func TestAgentMachineAuthenticationAndWire(t *testing.T){
    for _,operation:=range []string{"info","status","send"}{for _,auth:=range []string{"none","mfa","denied","cancelled"}{t.Run(operation+"/"+auth,func(t *testing.T){
        s:=store.Store{Root:filepath.Join(t.TempDir(),"registry")};p,_,err:=s.AddAgent("user","example.com",7263,"subsystem");if err!=nil{t.Fatal(err)}
        before,_:=s.List();r:=&agentFake{mfa:auth!="none",denyMFA:auth=="denied",promptError:auth=="cancelled"}
        cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
        args:=[]string{"agent",operation,p.Name};if operation=="send"{args=append(args,"hello \"world\"\nnext","--context-id","context-1")}
        var out bytes.Buffer;cmd.SetOut(&out);cmd.SetErr(io.Discard);cmd.SetArgs(args)
        err=cmd.Execute();failed:=auth=="denied"||auth=="cancelled";if (err!=nil)!=failed{t.Fatal(err)}
        var result struct{OK bool;Result json.RawMessage;Error string};decoder:=json.NewDecoder(&out);if err:=decoder.Decode(&result);err!=nil{t.Fatal(err)};if result.OK==failed{t.Fatal(result)};var extra any;if decoder.Decode(&extra)!=io.EOF{t.Fatal("more than one JSON document")}
        if strings.Contains(result.Error,"fixture-code"){t.Fatal("MFA leaked")}
        r.mu.Lock();methods:=append([]string(nil),r.methods...);params:=append([]json.RawMessage(nil),r.params...);r.mu.Unlock()
        expected:=[]string{"initialize"};if auth!="none"&&auth!="cancelled"{expected=append(expected,"auth.verify")};method:="agent."+operation;if operation=="send"{method="message/send"};if !failed{expected=append(expected,method)}
        if !reflect.DeepEqual(methods,expected){t.Fatal(methods,expected)}
        var initialize struct{Version int;Tools []any};json.Unmarshal(params[0],&initialize);if initialize.Version!=1||len(initialize.Tools)!=0{t.Fatal("unexpected shared tools",initialize)}
        if operation=="send"&&!failed{
            var sent struct{Message struct{Kind,Role,MessageID,ContextID string;Parts []struct{Kind,Text string}}}
            if err:=json.Unmarshal(params[len(params)-1],&sent);err!=nil{t.Fatal(err)}
            m:=sent.Message;if m.Kind!="message"||m.Role!="user"||len(m.MessageID)!=32||m.ContextID!="context-1"||len(m.Parts)!=1||m.Parts[0].Kind!="text"||m.Parts[0].Text!="hello \"world\"\nnext"{t.Fatal(sent)}
        }
        after,_:=s.List();if !reflect.DeepEqual(before,after){t.Fatal("machine command changed registry")}
    })}}
}
func TestAgentMachineRejectsInvalidTargetAndArguments(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"registry")};p,_,err:=s.Add("user","example.com",22);if err!=nil{t.Fatal(err)}
    for _,args:=range [][]string{{"agent","status",p.Name},{"agent","status","missing"},{"agent","status"},{"agent","send",p.Name,""}}{
        r:=&agentFake{};cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};var out bytes.Buffer;cmd.SetOut(&out);cmd.SetErr(io.Discard);cmd.SetArgs(args)
        if cmd.Execute()==nil{t.Fatal(args)};if !json.Valid(out.Bytes()){t.Fatal(out.String())};if len(r.methods)!=0{t.Fatal("transport started for invalid command")}
    }
}

func TestNamedMachineIdentityPin(t *testing.T){
    for _,instance:=range []string{"fixture-instance","replacement"}{t.Run(instance,func(t *testing.T){
        s:=store.Store{Root:filepath.Join(t.TempDir(),"registry")};source,_,err:=s.AddAgent("user","example.com",7263,"stdio");if err!=nil{t.Fatal(err)}
        named,err:=s.RegisterNamedAgent(source,"test","fixture-instance","opencode");if err!=nil{t.Fatal(err)}
        r:=&agentFake{infoType:"opencode",infoInstance:instance};cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
        cmd.SetArgs([]string{"agent","status",named.ID});cmd.SetOut(io.Discard);cmd.SetErr(io.Discard);err=cmd.Execute()
        if (err==nil)!=(instance=="fixture-instance"){t.Fatal(err)}
        r.mu.Lock();defer r.mu.Unlock();for _,method:=range r.methods{if instance!="fixture-instance"&&method=="agent.status"{t.Fatal("called replaced instance")}}
    })}
}
