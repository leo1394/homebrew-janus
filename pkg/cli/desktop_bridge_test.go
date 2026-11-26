package cli

import (
    "bytes"
    "encoding/json"
    "io"
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
