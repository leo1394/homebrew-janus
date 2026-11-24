package a2x

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "errors"
)

type Capabilities struct { Type string `json:"type"`; Terminal bool `json:"terminal"`; MFARequired bool `json:"mfaRequired"` }
type TerminalOptions struct { SessionID string `json:"sessionId"`; Cols int `json:"cols"`; Rows int `json:"rows"`; Term string `json:"term,omitempty"`; ColorTerm string `json:"colorTerm,omitempty"`; NoColor bool `json:"noColor"` }
type TerminalEvent struct { Data []byte; Exit bool; Code int }
func (c *Client) Capabilities() Capabilities {c.mu.Lock();defer c.mu.Unlock();return c.capabilities}
func (c *Client) Verify(ctx context.Context,code string)error{
    if err:=c.Call(ctx,"auth.verify",map[string]string{"code":code},nil);err!=nil{return errors.New("MFA verification failed")}
    return nil
}
// Enable delivery before terminal.open: servers may emit data before its reply.
func (c *Client) TerminalEvents() (<-chan TerminalEvent,error){
    c.mu.Lock();defer c.mu.Unlock()
    if !c.capabilities.Terminal{return nil,errors.New("server does not support native terminal UI; upgrade Agenux")}
    if c.session==""{return nil,errors.New("no active session")}
    if c.terminal==nil{c.terminal=make(chan TerminalEvent,32)}
    return c.terminal,nil
}
func (c *Client) OpenTerminal(ctx context.Context,cols,rows int)error{
    return c.OpenTerminalWithOptions(ctx,TerminalOptions{Cols:cols,Rows:rows})
}
func (c *Client) OpenTerminalWithOptions(ctx context.Context,options TerminalOptions)error{
    c.mu.Lock();options.SessionID=c.session;c.mu.Unlock()
    if options.Cols<1||options.Rows<1{return errors.New("invalid terminal size")}
    return c.Call(ctx,"terminal.open",options,nil)
}
func (c *Client) TerminalInput(data []byte)error{
    c.mu.Lock();id:=c.session;c.mu.Unlock()
    return c.Notify("terminal.input",map[string]string{"sessionId":id,"data":base64.StdEncoding.EncodeToString(data)})
}
func (c *Client) TerminalResize(cols,rows int)error{
    c.mu.Lock();id:=c.session;c.mu.Unlock()
    if cols<1||rows<1{return errors.New("invalid terminal size")}
    return c.Notify("terminal.resize",map[string]any{"sessionId":id,"cols":cols,"rows":rows})
}
func (c *Client) receiveTerminal(m message)error{
    var params struct{SessionID string `json:"sessionId"`;Data string `json:"data"`;Code *int `json:"code"`}
    if json.Unmarshal(m.Params,&params)!=nil{return errors.New("invalid terminal event")}
    c.mu.Lock();session,ch:=c.session,c.terminal;c.mu.Unlock()
    if ch==nil||session==""||session!=params.SessionID{return nil}
    event:=TerminalEvent{}
    if m.Method=="terminal.exit" {
        if params.Code==nil{return errors.New("terminal exit code missing")};event.Exit=true;event.Code=*params.Code
    } else {
        if len(params.Data)>350000{return errors.New("terminal data frame too large")}
        data,err:=base64.StdEncoding.DecodeString(params.Data);if err!=nil{return errors.New("invalid terminal base64")};event.Data=data
    }
    select{case ch<-event:return nil;case <-c.ctx.Done():return c.ctx.Err()}
}
func (c *Client) Done() <-chan struct{} {return c.done}
func (c *Client) Err()error{c.mu.Lock();defer c.mu.Unlock();return c.err}
