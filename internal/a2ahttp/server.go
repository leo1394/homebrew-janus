// Package a2ahttp exposes a fixed delegated grant on loopback HTTP only.
package a2ahttp

import (
    "context"
    "crypto/subtle"
    "crypto/sha256"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "mime"
    "net"
    "net/http"
    "strings"
    "sync"
    "time"

    "github.com/leo1394/homebrew-janus/internal/agentservice"
)
type Delegate func(context.Context,string,json.RawMessage)(agentservice.Response,error)
type Adapter struct { Credential string; URL string; Call Delegate; PollInterval time.Duration; slots chan struct{}; cardMu sync.Mutex; cardTag string; cardModified time.Time }
type request struct { Version string `json:"jsonrpc"`; ID json.RawMessage `json:"id"`; Method string `json:"method"`; Params json.RawMessage `json:"params"` }
type rpcError struct { Code int `json:"code"`; Message string `json:"message"`; Data []errorInfo `json:"data,omitempty"` }
type errorInfo struct { Type string `json:"@type"`; Domain string `json:"domain"`; Reason string `json:"reason"` }
type response struct { Version string `json:"jsonrpc"`; ID json.RawMessage `json:"id"`; Result json.RawMessage `json:"result,omitempty"`; Error *rpcError `json:"error,omitempty"` }
func New(credential string,call Delegate)*Adapter{return &Adapter{Credential:credential,Call:call,PollInterval:time.Second,slots:make(chan struct{},8)}}
func message(code int)string{
    switch code{case -32700:return "Invalid JSON payload";case -32600:return "Invalid request";case -32601:return "Method not found";case -32602:return "Invalid parameters";case -32603:return "Internal error";case -32001:return "Task not found";case -32002:return "Task not cancelable";case -32003:return "Push notifications not supported";case -32004:return "Unsupported operation";case -32005:return "Content type not supported";case -32006:return "Invalid agent response";case -32007:return "Extended agent card not configured";case -32008:return "Required extension not supported";case -32009:return "Version not supported"};return "Agent request rejected"
}
func failure(id json.RawMessage,code int)response{if len(id)==0{id=json.RawMessage(`null`)};return response{Version:"2.0",ID:id,Error:protocolError(code)}}
func(a *Adapter)invoke(ctx context.Context,method string,params json.RawMessage)(json.RawMessage,int){
    result,err:=a.Call(ctx,method,params);if err!=nil{return nil,-32603};if !result.OK{code:=result.Code;if code==0{code=-32000};return nil,code}
    if !json.Valid(result.Result){return nil,-32006};return result.Result,0
}
func(a *Adapter)card(data json.RawMessage)(json.RawMessage,int){
    var card map[string]json.RawMessage;if json.Unmarshal(data,&card)!=nil||card==nil{return nil,-32006}
    set:=func(key string,value any){card[key],_=json.Marshal(value)}
    set("supportedInterfaces",[]map[string]string{{"url":a.URL+"/rpc","protocolBinding":"JSONRPC","protocolVersion":"1.0"}})
    requirements:=[]any{map[string]any{"schemes":map[string]any{"janusGrant":map[string]any{"list":[]string{}}}}}
    set("securitySchemes",map[string]any{"janusGrant":map[string]any{"httpAuthSecurityScheme":map[string]string{"scheme":"Bearer"}}})
    set("securityRequirements",requirements)
    var skills []map[string]json.RawMessage
    if json.Unmarshal(card["skills"],&skills)==nil{for _,skill:=range skills{if _,exists:=skill["securityRequirements"];exists{skill["securityRequirements"],_=json.Marshal(requirements)}};set("skills",skills)}
    delete(card,"signatures") // Rewriting invalidates upstream signatures.
    out,err:=json.Marshal(card);if err!=nil{return nil,-32006};return out,0
}
func(a *Adapter)ServeHTTP(w http.ResponseWriter,r *http.Request){
    w.Header().Set("Cache-Control","no-store");w.Header().Set("X-Content-Type-Options","nosniff")
    expected:=strings.TrimPrefix(a.URL,"http://")
    if r.Host!=expected||r.Header.Get("Origin")!=""&&r.Header.Get("Origin")!=a.URL{http.Error(w,"forbidden origin or host",http.StatusForbidden);return}
    token:=strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ")
    if a.Credential==""||!strings.HasPrefix(r.Header.Get("Authorization"),"Bearer ")||subtle.ConstantTimeCompare([]byte(token),[]byte(a.Credential))!=1{w.Header().Set("WWW-Authenticate",`Bearer realm="Janus delegated agent"`);http.Error(w,"authorization required",401);return}
    select{case a.slots<-struct{}{}:defer func(){<-a.slots}();default:http.Error(w,"adapter busy",503);return}
    jsonReply:=func(value response){w.Header().Set("Content-Type","application/json");json.NewEncoder(w).Encode(value)}
    if r.Method=="GET"&&r.URL.Path=="/.well-known/agent-card.json"{
        data,code:=a.invoke(r.Context(),"agent.card",json.RawMessage(`{}`));if code==0{data,code=a.card(data)};if code!=0{jsonReply(failure(nil,code));return}
        // Derive the validator from the final local representation only after
        // the remote grant check. Revoked capabilities must never receive 304.
        tag:=fmt.Sprintf(`"%x"`,sha256.Sum256(data))
        w.Header().Set("Cache-Control","private, max-age=0, must-revalidate")
        w.Header().Set("Vary","Authorization")
        a.cardMu.Lock()
        if a.cardTag!=tag{a.cardTag=tag;a.cardModified=time.Now().UTC().Truncate(time.Second)}
        modified:=a.cardModified;a.cardMu.Unlock()
        w.Header().Set("Last-Modified",modified.Format(http.TimeFormat))
        w.Header().Set("ETag",tag)
        w.Header().Set("Content-Type","application/json")
        for _,value:=range r.Header.Values("If-None-Match"){for _,candidate:=range strings.Split(value,","){candidate=strings.TrimSpace(candidate);if candidate=="*"||strings.TrimPrefix(candidate,"W/")==tag{w.WriteHeader(http.StatusNotModified);return}}}
        w.Write(data);return
    }
    if r.Method!="POST"||(r.URL.Path!="/rpc"&&r.URL.Path!="/rpc/"&&r.URL.Path!="/"){http.NotFound(w,r);return}
    media,_,_:=mime.ParseMediaType(r.Header.Get("Content-Type"));if media!="application/json"{http.Error(w,"application/json required",415);return}
    if version:=r.Header.Get("A2A-Version");version!=""&&version!="1.0"{jsonReply(failure(nil,-32009));return}
    if r.Header.Get("A2A-Extensions")!=""{jsonReply(failure(nil,-32008));return} // No extension negotiation over A2X yet.
    body,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,1<<20));if err!=nil{jsonReply(failure(nil,-32600));return};if !json.Valid(body){jsonReply(failure(nil,-32700));return}
    var q request
    if json.Unmarshal(body,&q)!=nil||q.Version!="2.0"||!validID(q.ID)||q.Method==""{jsonReply(failure(nil,-32600));return}
    if !agentservice.IsA2AMethod(q.Method){jsonReply(failure(q.ID,-32601));return}
    if len(q.Params)==0{q.Params=json.RawMessage(`{}`)}
    var params map[string]json.RawMessage;if json.Unmarshal(q.Params,&params)!=nil||params==nil{jsonReply(failure(q.ID,-32602));return}
    var data json.RawMessage;var code int
    if q.Method=="SendMessage"{data,code=a.sendMessage(r.Context(),q.Params)}else{data,code=a.invoke(r.Context(),q.Method,q.Params)}
    if code!=0{jsonReply(failure(q.ID,code));return}
    if q.Method=="GetExtendedAgentCard"{data,code=a.card(data);if code!=0{jsonReply(failure(q.ID,code));return}}
    if q.Method=="SendStreamingMessage"||q.Method=="SubscribeToTask"{a.stream(w,r,q.ID,data);return}
    jsonReply(response{Version:"2.0",ID:q.ID,Result:data})
}
func validID(id json.RawMessage)bool{
    if len(id)==0||string(id)=="null"{return false}
    var s string;if json.Unmarshal(id,&s)==nil{return true}
    var n json.Number;return json.Unmarshal(id,&n)==nil
}
func(a *Adapter)stream(w http.ResponseWriter,r *http.Request,id json.RawMessage,initial json.RawMessage){
    var first struct{Task json.RawMessage `json:"task"`;Message json.RawMessage `json:"message"`;Cursor int64 `json:"cursor"`}
    if json.Unmarshal(initial,&first)!=nil||first.Cursor<0||(len(first.Task)==0)==(len(first.Message)==0){w.Header().Set("Content-Type","application/json");json.NewEncoder(w).Encode(failure(id,-32006));return}
    var task struct{ID string `json:"id"`}
    if len(first.Task)>0&&(json.Unmarshal(first.Task,&task)!=nil||task.ID==""){w.Header().Set("Content-Type","application/json");json.NewEncoder(w).Encode(failure(id,-32006));return}
    w.Header().Set("Content-Type","text/event-stream");w.Header().Set("X-Accel-Buffering","no")
    controller:=http.NewResponseController(w)
    send:=func(value response)bool{
        controller.SetWriteDeadline(time.Now().Add(10*time.Second))
        data,err:=json.Marshal(value);if err!=nil{return false};if _,err=fmt.Fprintf(w,"data: %s\n\n",data);err!=nil{return false};return controller.Flush()==nil
    }
    payload:=map[string]json.RawMessage{};if len(first.Task)>0{payload["task"]=first.Task}else{payload["message"]=first.Message};data,_:=json.Marshal(payload)
    if !send(response{Version:"2.0",ID:id,Result:data})||task.ID==""{return}
    after:=first.Cursor;interval:=a.PollInterval;if interval<=0{interval=time.Second}
    for {
        params,_:=json.Marshal(map[string]any{"id":task.ID,"after":after})
        data,code:=a.invoke(r.Context(),"a2x.events",params);if code!=0{if r.Context().Err()==nil{send(failure(id,code))};return}
        var page struct{Events []struct{Seq int64 `json:"seq"`;Event json.RawMessage `json:"event"`} `json:"events"`;Terminal *bool `json:"terminal"`}
        if json.Unmarshal(data,&page)!=nil||page.Terminal==nil||page.Events==nil||len(page.Events)>4096{send(failure(id,-32006));return}
        for _,event:=range page.Events{
            var fields map[string]json.RawMessage;var update struct{TaskID string `json:"taskId"`}
            if event.Seq<=after||json.Unmarshal(event.Event,&fields)!=nil||len(fields)!=1{send(failure(id,-32006));return}
            value,ok:=fields["statusUpdate"];if !ok{value,ok=fields["artifactUpdate"]}
            if !ok||json.Unmarshal(value,&update)!=nil||update.TaskID!=task.ID{send(failure(id,-32006));return}
            if !send(response{Version:"2.0",ID:id,Result:event.Event}){return};after=event.Seq
        }
        if *page.Terminal{return}
        timer:=time.NewTimer(interval);select{case <-r.Context().Done():timer.Stop();return;case <-timer.C:}
    }
}
// Serve accepts numeric loopback addresses only. URL is fixed before serving and
// never derived from untrusted Host headers. There is no remote HTTP listener.
func Serve(ctx context.Context,address string,a *Adapter,ready func(string))error{
    host,_,err:=net.SplitHostPort(address);if err!=nil{return err};if host!="127.0.0.1"&&host!="::1"{return errors.New("A2A HTTP must bind to 127.0.0.1 or ::1")}
    listener,err:=net.Listen("tcp",address);if err!=nil{return err};defer listener.Close()
    a.URL="http://"+listener.Addr().String()
    server:=&http.Server{Handler:a,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,IdleTimeout:30*time.Second,MaxHeaderBytes:8192,BaseContext:func(net.Listener)context.Context{return ctx}}
    defer server.Close();stop:=context.AfterFunc(ctx,func(){server.Close()});defer stop()
    if ready!=nil{ready(a.URL)}
    err=server.Serve(listener);if ctx.Err()!=nil{return nil};return err
}

func protocolError(code int)*rpcError{
    result:=&rpcError{Code:code,Message:message(code)}
    reasons:=map[int]string{-32001:"TASK_NOT_FOUND",-32002:"TASK_NOT_CANCELABLE",-32003:"PUSH_NOTIFICATION_NOT_SUPPORTED",-32004:"UNSUPPORTED_OPERATION",-32005:"CONTENT_TYPE_NOT_SUPPORTED",-32006:"INVALID_AGENT_RESPONSE",-32007:"EXTENDED_AGENT_CARD_NOT_CONFIGURED",-32008:"EXTENSION_SUPPORT_REQUIRED",-32009:"VERSION_NOT_SUPPORTED"}
    if reason:=reasons[code];reason!=""{result.Data=[]errorInfo{{Type:"type.googleapis.com/google.rpc.ErrorInfo",Domain:"a2a-protocol.org",Reason:reason}}}
    return result
}
