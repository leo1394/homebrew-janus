package cli

import (
    "bytes"
    "encoding/json"
    "io"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/store"
)

func TestDesktopBridgeConversation(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"registry")}
    p,_,err:=s.AddAgent("owner","example.com",7263,"stdio");if err!=nil{t.Fatal(err)}
    r:=&agentFake{mfa:true}
    cmd:=bridgeCommand(s,r)
    cmd.SetArgs([]string{p.ID})
    cmd.SetIn(strings.NewReader("{\"id\":\"1\",\"method\":\"open\"}\n{\"id\":\"2\",\"method\":\"send\",\"session\":\"s1\",\"text\":\"hello\"}\n"))
    var out bytes.Buffer;cmd.SetOut(&out)
    if err:=cmd.Execute();err!=nil{t.Fatal(err)}
    dec:=json.NewDecoder(&out)
    var ready,opened,reply map[string]any
    for _,v:=range []*map[string]any{&ready,&opened,&reply}{if err:=dec.Decode(v);err!=nil{t.Fatal(err)}}
    if ready["event"]!="ready"||ready["instance"]!="fixture-instance"||opened["value"]!="s1"||reply["id"]!="2"||reply["value"]!="hello"{t.Fatal(ready,opened,reply)}
    if dec.Decode(&reply)!=io.EOF{t.Fatal("unexpected event")}
}

func TestBridgeHelpAndDiscovery(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"registry")}
    for _,args:=range [][]string{{"--help"},{"help","bridge"},{"bridge","--help"}}{
        out,err:=execute(t,s,args...);if err!=nil||!strings.Contains(out,"bridge"){t.Fatal(out,err)}
        if len(args)>1&&!strings.Contains(out,"newline-delimited JSON"){t.Fatal("missing bridge description",out)}
    }
    out,err:=execute(t,s,"bridge");if err!=nil{t.Fatal(err)}
    var discovery struct{Version int;Devices []any};if err:=json.Unmarshal([]byte(out),&discovery);err!=nil||discovery.Version!=1||len(discovery.Devices)!=0{t.Fatal(out,err)}
    if _,err:=execute(t,s,"desktop-bridge");err==nil{t.Fatal("old command still accepted")}
}

func TestBridgeRefreshesSessionTitles(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"registry")}
    p,_,err:=s.AddAgent("owner","example.com",7263,"stdio");if err!=nil{t.Fatal(err)}
    cmd:=bridgeCommand(s,&agentFake{})
    cmd.SetArgs([]string{p.ID});cmd.SetIn(strings.NewReader("{\"id\":\"refresh\",\"method\":\"sessions\"}\n"))
    var out bytes.Buffer;cmd.SetOut(&out);if err:=cmd.Execute();err!=nil{t.Fatal(err)}
    decoder:=json.NewDecoder(&out);var ready map[string]any;if err:=decoder.Decode(&ready);err!=nil{t.Fatal(err)}
    var reply struct{ID string `json:"id"`;Value []struct{ID,Name,Title string} `json:"value"`}
    if err:=decoder.Decode(&reply);err!=nil{t.Fatal(err)}
    if reply.ID!="refresh"||len(reply.Value)!=1||reply.Value[0].Title!="Server title"{t.Fatal(reply)}
}

func TestBridgePrincipalAndLastSessionCompatibility(t *testing.T){
    for _,isolated:=range []bool{false,true}{
        s:=store.Store{Root:filepath.Join(t.TempDir(),"registry")};p,_,err:=s.AddAgent("owner","example.com",7263,"stdio");if err!=nil{t.Fatal(err)}
        if err:=s.SetSession(p.ID,"foreign-session");err!=nil{t.Fatal(err)}
        cmd:=bridgeCommand(s,&agentFake{isolated:isolated});cmd.SetArgs([]string{p.ID});cmd.SetIn(strings.NewReader(""))
        var out bytes.Buffer;cmd.SetOut(&out);if err:=cmd.Execute();err!=nil{t.Fatal(err)}
        var ready struct{Last string;WorkspaceIsolation string;Principal *struct{ID,Username string}}
        if err:=json.NewDecoder(&out).Decode(&ready);err!=nil{t.Fatal(err)}
        if ready.Last!=""{t.Fatal("foreign LastSession leaked")}
        if isolated{if ready.WorkspaceIsolation!="principal-v1"||ready.Principal==nil||ready.Principal.ID!="principal-fixture"{t.Fatal("principal not forwarded")}}else if ready.WorkspaceIsolation!=""||ready.Principal!=nil{t.Fatal("legacy server claimed isolation")}
        data,err:=os.ReadFile(filepath.Join(s.Root,"devices.json"));if err!=nil{t.Fatal(err)}
        if bytes.Contains(data,[]byte("principal"))||bytes.Contains(data,[]byte("workspaceIsolation")){t.Fatal("connection identity entered registry")}
    }
}
