// Package a2x implements the Agenux v1 newline-delimited JSON-RPC transport.
package a2x

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "sync"
    "sort"
)

type Tool struct {
    Name string `json:"name"`
    Description string `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"`
}
type ToolCall struct {
    SessionID string `json:"sessionId"`
    Name string `json:"name"`
    Arguments json.RawMessage `json:"arguments"`
}
type Content struct { Type string `json:"type"`; Text string `json:"text"` }
type ToolResult struct { Content []Content `json:"content"` }
type SharedTool struct { Tool; Call func(context.Context, ToolCall) (ToolResult,error) }
type Session struct { ID string `json:"id"`; Name string `json:"name"` }
type Delta struct { SessionID string `json:"sessionId"`; Text string `json:"text"` }
type rpcError struct { Code int `json:"code"`; Message string `json:"message"` }
func(e *rpcError)Error()string{return fmt.Sprintf("A2X error %d: %s",e.Code,e.Message)}
type message struct {
    Version string `json:"jsonrpc"`
    ID json.RawMessage `json:"id,omitempty"`
    Method string `json:"method,omitempty"`
    Params json.RawMessage `json:"params,omitempty"`
    Result json.RawMessage `json:"result,omitempty"`
    Error *rpcError `json:"error,omitempty"`
}
type Client struct {
    stream io.ReadWriteCloser
    ctx context.Context
    cancel context.CancelFunc
    writeMu sync.Mutex
    mu sync.Mutex
    next uint64
    pending map[string]chan message
    tools map[string]SharedTool
    session string
    done chan struct{}
    err error
    delta func(Delta)
    slots chan struct{}
    capabilities Capabilities
    terminal chan TerminalEvent
}
// New shares only the supplied capabilities. It never discovers local tools.
func New(stream io.ReadWriteCloser, tools []SharedTool, delta func(Delta)) (*Client,error) {
    ctx,cancel:=context.WithCancel(context.Background())
    c:=&Client{stream:stream,ctx:ctx,cancel:cancel,pending:map[string]chan message{},tools:map[string]SharedTool{},done:make(chan struct{}),delta:delta,slots:make(chan struct{},8)}
    for _,tool:=range tools {
        if tool.Name=="" || tool.Call==nil || !json.Valid(tool.InputSchema) { cancel();return nil,errors.New("invalid shared tool") }
        if _,exists:=c.tools[tool.Name];exists { cancel();return nil,errors.New("duplicate shared tool") }
        c.tools[tool.Name]=tool
    }
    go c.read()
    return c,nil
}
func (c *Client) Close() error { c.cancel();return c.stream.Close() }
func (c *Client) write(m message) error {
    c.writeMu.Lock();defer c.writeMu.Unlock()
    m.Version="2.0"
    return json.NewEncoder(c.stream).Encode(m)
}
func (c *Client) read() {
    var failure error=io.EOF
    defer func(){c.mu.Lock();c.err=failure;c.mu.Unlock();c.cancel();close(c.done);c.stream.Close()}()
    scanner:=bufio.NewScanner(c.stream);scanner.Buffer(make([]byte,4096),4<<20)
    for scanner.Scan() {
        var m message
        if err:=json.Unmarshal(scanner.Bytes(),&m);err!=nil || m.Version!="2.0" { failure=errors.New("invalid A2X JSON-RPC frame");return }
        if m.Method!="" {
            if len(m.ID)>0 {
                select {case c.slots<-struct{}{}:go func(){defer func(){<-c.slots}();c.handle(m)}();default:failure=errors.New("too many concurrent server requests");return}
            } else if m.Method=="terminal.data" || m.Method=="terminal.exit" {
                if err:=c.receiveTerminal(m);err!=nil{failure=err;return}
            } else if m.Method=="task.delta" {
                var d Delta
                if json.Unmarshal(m.Params,&d)!=nil { failure=errors.New("invalid task.delta");return }
                c.mu.Lock();session:=c.session;c.mu.Unlock()
                if d.SessionID==session && session!="" && c.delta!=nil { c.delta(d) }
            }
            continue
        }
        c.mu.Lock();ch:=c.pending[string(m.ID)];c.mu.Unlock()
        if ch!=nil { select {case ch<-m:default:} }
    }
    if err:=scanner.Err();err!=nil { failure=err }
}
func (c *Client) handle(m message) {
    response:=message{ID:m.ID}
    var call ToolCall
    c.mu.Lock();session:=c.session;c.mu.Unlock()
    if m.Method=="ping" { response.Result=json.RawMessage(`{}`) } else if m.Method!="clientTool.call" { response.Error=&rpcError{-32601,"method not found"} } else if json.Unmarshal(m.Params,&call)!=nil || session=="" || call.SessionID!=session {
        response.Error=&rpcError{-32602,"invalid session or parameters"}
    } else if tool,ok:=c.tools[call.Name];!ok {
        response.Error=&rpcError{-32601,"tool was not explicitly shared"}
    } else {
        result,err:=tool.Call(c.ctx,call)
        if err!=nil { response.Error=&rpcError{-32000,"local tool failed"} } else {
            response.Result,err=json.Marshal(result)
            if err!=nil { response.Error=&rpcError{-32603,"invalid tool result"} }
        }
    }
    if err:=c.write(response);err!=nil { c.Close() }
}
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
    if err:=ctx.Err();err!=nil{return err}
    stop:=context.AfterFunc(ctx,func(){c.Close()});defer stop()
    data,err:=json.Marshal(params);if err!=nil{return err}
    c.mu.Lock();c.next++;id:=fmt.Sprint(c.next);ch:=make(chan message,1);c.pending[id]=ch;c.mu.Unlock()
    defer func(){c.mu.Lock();delete(c.pending,id);c.mu.Unlock()}()
    if err:=c.write(message{ID:json.RawMessage(id),Method:method,Params:data});err!=nil{return err}
    var m message
    select {
    case <-ctx.Done(): c.Close();return ctx.Err()
    case <-c.done:
        select {case m=<-ch:default:c.mu.Lock();err:=c.err;c.mu.Unlock();return err}
    case m=<-ch:
    }
    {
        if m.Error!=nil{return m.Error}
        if len(m.Result)==0{return errors.New("missing JSON-RPC result")}
        if result==nil{return nil};return json.Unmarshal(m.Result,result)
    }
}
func (c *Client) Initialize(ctx context.Context) error {
    tools:=make([]Tool,0,len(c.tools));for _,tool:=range c.tools{tools=append(tools,tool.Tool)}
    sort.Slice(tools,func(i,j int)bool{return tools[i].Name<tools[j].Name})
    var capabilities Capabilities
    if err:=c.Call(ctx,"initialize",struct{Version int `json:"version"`;Tools []Tool `json:"tools"`}{1,tools},&capabilities);err!=nil{return err}
    c.mu.Lock();c.capabilities=capabilities;c.mu.Unlock();return nil
}
func (c *Client) Sessions(ctx context.Context) ([]Session,error) {
    var result struct{Sessions []Session `json:"sessions"`}
    err:=c.Call(ctx,"session.list",struct{}{},&result);return result.Sessions,err
}
func (c *Client) Open(ctx context.Context, id string) (string,error) {
    method:="session.open";params:=map[string]string{}
    if id!="" {method="session.resume";params["sessionId"]=id}
    var result struct{SessionID string `json:"sessionId"`}
    if err:=c.Call(ctx,method,params,&result);err!=nil{return "",err}
    if result.SessionID=="" {return "",errors.New("server returned empty session ID")}
    c.mu.Lock();c.session=result.SessionID;c.mu.Unlock();return result.SessionID,nil
}
func (c *Client) Send(ctx context.Context,text string) (string,error) {
    c.mu.Lock();id:=c.session;c.mu.Unlock();if id==""{return "",errors.New("no active session")}
    var result struct{Text string `json:"text"`}
    err:=c.Call(ctx,"task.send",map[string]string{"sessionId":id,"text":text},&result);return result.Text,err
}

func (c *Client) Notify(method string,params any) error { data,err:=json.Marshal(params);if err!=nil{return err};return c.write(message{Method:method,Params:data}) }
