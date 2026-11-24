// Package mcpshare starts only explicitly configured local MCP processes and
// exposes only exact allowlisted names. Configuration is never read remotely.
package mcpshare

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "sync"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
)
type Server struct {
    Command string `json:"command"`
    Args []string `json:"args"`
    Tools []string `json:"tools"`
}
type Config struct { Servers []Server `json:"servers"` }
type process struct { io.ReadCloser; input io.WriteCloser; cmd *exec.Cmd; once sync.Once }
func (p *process) Write(b []byte)(int,error){return p.input.Write(b)}
func (p *process) Close()error{p.once.Do(func(){p.input.Close();p.ReadCloser.Close();p.cmd.Process.Kill();p.cmd.Wait()});return nil}
func Load(ctx context.Context,path string,stderr io.Writer)(tools []a2x.SharedTool, closeAll func(), err error){
    clients:=[]*a2x.Client{}
    closeAll=func(){for _,client:=range clients{client.Close()}}
    defer func(){if err!=nil{closeAll()}}()
    if path==""{return nil,closeAll,nil}
    file,err:=os.Open(path);if err!=nil{return nil,closeAll,err};defer file.Close()
    var config Config
    decoder:=json.NewDecoder(io.LimitReader(file,1<<20));decoder.DisallowUnknownFields()
    if err=decoder.Decode(&config);err!=nil{return nil,closeAll,err}
    var extra any;if decoder.Decode(&extra)!=io.EOF{return nil,closeAll,errors.New("trailing MCP configuration")}
    if len(config.Servers)>16{return nil,closeAll,errors.New("too many MCP servers")}
    names:=map[string]bool{}
    for _,server:=range config.Servers {
        if !filepath.IsAbs(server.Command)||len(server.Tools)==0{return nil,closeAll,errors.New("MCP requires an absolute executable and nonempty exact tools allowlist")}
        for _,name:=range server.Tools{if name==""||names[name]{return nil,closeAll,errors.New("empty or duplicate shared tool name")};names[name]=true}
    }
    for _,server:=range config.Servers {
        command:=exec.CommandContext(ctx,server.Command,server.Args...);command.Stderr=stderr
        input,e:=command.StdinPipe();if e!=nil{return nil,closeAll,e}
        output,e:=command.StdoutPipe();if e!=nil{input.Close();return nil,closeAll,e}
        if e=command.Start();e!=nil{input.Close();output.Close();return nil,closeAll,e}
        client,e:=a2x.New(&process{ReadCloser:output,input:input,cmd:command},nil,nil);if e!=nil{return nil,closeAll,e};clients=append(clients,client)
        setup,cancel:=context.WithTimeout(ctx,15*time.Second)
        var initialized struct{ProtocolVersion string `json:"protocolVersion"`;Capabilities struct{Tools json.RawMessage `json:"tools"`} `json:"capabilities"`}
        e=client.Call(setup,"initialize",map[string]any{"protocolVersion":"2025-03-26","capabilities":map[string]any{},"clientInfo":map[string]string{"name":"janus","version":"1"}},&initialized)
        if e==nil && (initialized.ProtocolVersion!="2025-03-26"||len(initialized.Capabilities.Tools)==0){e=errors.New("MCP protocol or tools capability unsupported")}
        if e==nil{e=client.Notify("notifications/initialized",struct{}{})}
        discovered:=map[string]a2x.Tool{};cursor:=""
        for page:=0;e==nil;page++ {
            if page>=100{e=errors.New("MCP tool pagination limit exceeded");break}
            params:=map[string]string{};if cursor!=""{params["cursor"]=cursor}
            var result struct{Tools []a2x.Tool `json:"tools"`;Next string `json:"nextCursor"`}
            e=client.Call(setup,"tools/list",params,&result);if e!=nil{break}
            for _,tool:=range result.Tools{if _,exists:=discovered[tool.Name];exists{e=errors.New("duplicate MCP tool descriptor");break};discovered[tool.Name]=tool}
            if result.Next==""{break};cursor=result.Next
        }
        cancel();if e!=nil{return nil,closeAll,e}
        for _,name:=range server.Tools {
            tool,ok:=discovered[name];if !ok{return nil,closeAll,fmt.Errorf("allowlisted MCP tool not found: %s",name)}
            toolName:=name;peer:=client
            tools=append(tools,a2x.SharedTool{Tool:tool,Call:func(callCtx context.Context,call a2x.ToolCall)(a2x.ToolResult,error){
                bounded,cancel:=context.WithTimeout(callCtx,2*time.Minute);defer cancel()
                var result struct{Content []a2x.Content `json:"content"`;IsError bool `json:"isError"`}
                err:=peer.Call(bounded,"tools/call",map[string]any{"name":toolName,"arguments":call.Arguments},&result)
                if err!=nil{return a2x.ToolResult{},err};if result.IsError{return a2x.ToolResult{},errors.New("MCP tool failed")}
                for _,item:=range result.Content{if item.Type!="text"{return a2x.ToolResult{},errors.New("A2X v1 accepts text tool results only")}}
                return a2x.ToolResult{Content:result.Content},nil
            }})
        }
    }
    return tools,closeAll,nil
}
