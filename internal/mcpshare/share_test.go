package mcpshare

import (
    "context"
    "encoding/json"
    "io"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
)
func TestMCPProcess(t *testing.T){
    if os.Args[len(os.Args)-1]!="mcp-fixture"{return}
    dec:=json.NewDecoder(os.Stdin);enc:=json.NewEncoder(os.Stdout)
    for {var m struct{ID json.RawMessage `json:"id"`;Method string `json:"method"`;Params json.RawMessage `json:"params"`};if dec.Decode(&m)!=nil{os.Exit(0)}
        if len(m.ID)==0{continue}
        var result any
        switch m.Method {
        case "initialize":result=map[string]any{"protocolVersion":"2025-03-26","capabilities":map[string]any{"tools":map[string]any{}}}
        case "tools/list":result=map[string]any{"tools":[]map[string]any{{"name":"visible","description":"test","inputSchema":map[string]any{"type":"object"}},{"name":"hidden","inputSchema":map[string]any{"type":"object"}}}}
        case "tools/call":var p struct{Name string};json.Unmarshal(m.Params,&p);if p.Name!="visible"{os.Exit(3)};result=map[string]any{"content":[]map[string]string{{"type":"text","text":"local result"}}}
        default:os.Exit(4)
        }
        if enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"result":result})!=nil{os.Exit(2)}
    }
}
func TestExplicitMCPSharing(t *testing.T){
    executable,err:=os.Executable();if err!=nil{t.Fatal(err)}
    path:=filepath.Join(t.TempDir(),"share.json")
    config:=Config{Servers:[]Server{{Command:executable,Args:[]string{"-test.run=TestMCPProcess","--","mcp-fixture"},Tools:[]string{"visible"}}}}
    data,_:=json.Marshal(config);if err:=os.WriteFile(path,data,0600);err!=nil{t.Fatal(err)}
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
    tools,closeAll,err:=Load(ctx,path,io.Discard);if err!=nil{t.Fatal(err)};defer closeAll()
    if len(tools)!=1||tools[0].Name!="visible"{t.Fatal("unapproved tool shared",tools)}
    result,err:=tools[0].Call(ctx,a2x.ToolCall{SessionID:"s",Name:"visible",Arguments:json.RawMessage(`{}`)})
    if err!=nil||len(result.Content)!=1||result.Content[0].Text!="local result"{t.Fatal(result,err)}
    empty,closeEmpty,err:=Load(ctx,"",io.Discard);defer closeEmpty();if err!=nil||len(empty)!=0{t.Fatal(empty,err)}
    config.Servers[0].Tools=[]string{"missing"};data,_=json.Marshal(config);os.WriteFile(path,data,0600)
    if _,_,err:=Load(ctx,path,io.Discard);err==nil{t.Fatal("missing allowlisted tool accepted")}
}
