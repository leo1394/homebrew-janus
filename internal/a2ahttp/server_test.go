package a2ahttp

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/agentservice"
)
func fixture(call Delegate)*Adapter{a:=New("private-grant",call);a.URL="http://127.0.0.1:1234";a.PollInterval=time.Millisecond;return a}
func requestHTTP(a *Adapter,body,token string)*httptest.ResponseRecorder{
    r:=httptest.NewRequest("POST",a.URL+"/rpc",strings.NewReader(body));r.Header.Set("Content-Type","application/json");if token!=""{r.Header.Set("Authorization","Bearer "+token)};w:=httptest.NewRecorder();a.ServeHTTP(w,r);return w
}
func TestStandardMethodsAndSafeErrors(t *testing.T){
    methods:=[]string{"SendMessage","GetTask","ListTasks","CancelTask","CreateTaskPushNotificationConfig","GetTaskPushNotificationConfig","ListTaskPushNotificationConfigs","DeleteTaskPushNotificationConfig","GetExtendedAgentCard"}
    for _,method:=range methods{t.Run(method,func(t *testing.T){
        called:="";a:=fixture(func(_ context.Context,m string,p json.RawMessage)(agentservice.Response,error){called=m;result:=json.RawMessage(`{"name":"Agent","signatures":["obsolete"]}`);if m=="SendMessage"{result=json.RawMessage(`{"message":{"messageId":"reply"}}`)};return agentservice.Response{OK:true,Result:result},nil})
        w:=requestHTTP(a,`{"jsonrpc":"2.0","id":9007199254740993,"method":"`+method+`","params":{}}`,"private-grant")
        if w.Code!=200||called!=method||!strings.Contains(w.Body.String(),`"id":9007199254740993`){t.Fatal(w.Code,called,w.Body.String())}
        if method=="GetExtendedAgentCard"&&(strings.Contains(w.Body.String(),"obsolete")||!strings.Contains(w.Body.String(),a.URL+"/rpc")){t.Fatal(w.Body.String())}
    })}
    a:=fixture(func(context.Context,string,json.RawMessage)(agentservice.Response,error){return agentservice.Response{Code:-32003,Error:"private-grant internal path"},nil})
    w:=requestHTTP(a,`{"jsonrpc":"2.0","id":"x","method":"CreateTaskPushNotificationConfig","params":{}}`,"private-grant")
    if !strings.Contains(w.Body.String(),`"code":-32003`)||strings.Contains(w.Body.String(),"private-grant"){t.Fatal(w.Body.String())}
}
func TestHTTPAuthenticationAndProtocolValidation(t *testing.T){
    calls:=0;a:=fixture(func(context.Context,string,json.RawMessage)(agentservice.Response,error){calls++;return agentservice.Response{OK:true,Result:json.RawMessage(`{}`)},nil})
    for _,token:=range []string{"","wrong"}{if w:=requestHTTP(a,`{}`,token);w.Code!=401{t.Fatal(w.Code)}}
    for _,tc:=range []struct{body string;code string}{{`{`,"-32700"},{`[]`,"-32600"},{`{"jsonrpc":"2.0","id":true,"method":"GetTask"}`,"-32600"},{`{"jsonrpc":"2.0","id":1,"method":"terminal.open"}`,"-32601"},{`{"jsonrpc":"2.0","id":1,"method":"GetTask","params":[]}`,"-32602"}}{
        w:=requestHTTP(a,tc.body,"private-grant");if !strings.Contains(w.Body.String(),`"code":`+tc.code){t.Fatal(w.Body.String())}
    }
    r:=httptest.NewRequest("GET",a.URL+"/.well-known/agent-card.json",nil);r.Header.Set("Authorization","Bearer private-grant");r.Header.Set("Origin","https://hostile.example");w:=httptest.NewRecorder();a.ServeHTTP(w,r);if w.Code!=403{t.Fatal(w.Code)}
    r.Header.Del("Origin");r.Host="hostile.example";w=httptest.NewRecorder();a.ServeHTTP(w,r);if w.Code!=403{t.Fatal(w.Code)}
    if calls!=0{t.Fatal("invalid request reached grant",calls)}
    if err:=Serve(context.Background(),"0.0.0.0:0",a,nil);err==nil{t.Fatal("remote listener permitted")}
}
func TestStreamingCursorAndErrors(t *testing.T){
    for _,method:=range []string{"SendStreamingMessage","SubscribeToTask"}{t.Run(method,func(t *testing.T){
        calls:=0;a:=fixture(func(_ context.Context,m string,p json.RawMessage)(agentservice.Response,error){
            calls++;result:=`{"task":{"id":"t"},"cursor":7}`
            if m=="a2x.events"{var cursor struct{ID string;After int64};json.Unmarshal(p,&cursor);if cursor.ID!="t"||cursor.After!=7{t.Fatal(cursor)};result=`{"events":[{"seq":8,"event":{"artifactUpdate":{"taskId":"t","artifact":{"artifactId":"a","parts":[{"text":"hello"}]}}}},{"seq":9,"event":{"statusUpdate":{"taskId":"t","status":{"state":"TASK_STATE_COMPLETED"}}}}],"terminal":true}`}
            return agentservice.Response{OK:true,Result:json.RawMessage(result)},nil
        })
        w:=requestHTTP(a,`{"jsonrpc":"2.0","id":4,"method":"`+method+`","params":{}}`,"private-grant")
        body:=w.Body.String();if calls!=2||w.Header().Get("Content-Type")!="text/event-stream"||strings.Count(body,"data: ")!=3||strings.Contains(body,"cursor")||strings.Contains(body,`"seq"`){t.Fatal(calls,body)}
        for _,line:=range strings.Split(strings.TrimSpace(body),"\n\n"){var response response;if err:=json.Unmarshal([]byte(strings.TrimPrefix(line,"data: ")),&response);err!=nil{t.Fatal(err)};if string(response.ID)!="4"{t.Fatal(string(response.ID))}}
    })}
    a:=fixture(func(_ context.Context,m string,_ json.RawMessage)(agentservice.Response,error){if m=="a2x.events"{return agentservice.Response{Code:-32001,Error:"secret"},nil};return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t"}}`)},nil})
    w:=requestHTTP(a,`{"jsonrpc":"2.0","id":1,"method":"SubscribeToTask"}`,"private-grant");if !strings.Contains(w.Body.String(),`"code":-32001`)||strings.Contains(w.Body.String(),"secret"){t.Fatal(w.Body.String())}
}
func TestAgentCardLocalInterface(t *testing.T){
    a:=fixture(func(_ context.Context,method string,_ json.RawMessage)(agentservice.Response,error){if method!="agent.card"{t.Fatal(method)};return agentservice.Response{OK:true,Result:json.RawMessage(`{"name":"codex","supportedInterfaces":[{"url":"ssh://remote"}],"signatures":[{}]}`)},nil})
    r:=httptest.NewRequest(http.MethodGet,a.URL+"/.well-known/agent-card.json",nil);r.Header.Set("Authorization","Bearer private-grant");w:=httptest.NewRecorder();a.ServeHTTP(w,r)
    if strings.Contains(w.Body.String(),"ssh://")||strings.Contains(w.Body.String(),"signatures")||!strings.Contains(w.Body.String(),"httpAuthSecurityScheme"){t.Fatal(w.Body.String())}
}

func TestStreamRejectsReplayedOrForeignEvents(t *testing.T){
    for _,page:=range []string{`{"events":[{"seq":2,"event":{"statusUpdate":{"taskId":"t"}}}],"terminal":true}`,`{"events":[{"seq":3,"event":{"statusUpdate":{"taskId":"other"}}}],"terminal":true}`} {
        a:=fixture(func(_ context.Context,m string,_ json.RawMessage)(agentservice.Response,error){result:=`{"task":{"id":"t"},"cursor":2}`;if m=="a2x.events"{result=page};return agentservice.Response{OK:true,Result:json.RawMessage(result)},nil})
        w:=requestHTTP(a,`{"jsonrpc":"2.0","id":1,"method":"SubscribeToTask"}`,"private-grant");if !strings.Contains(w.Body.String(),`"code":-32006`){t.Fatal(w.Body.String())}
    }
}
func TestStreamCancellationStopsPolling(t *testing.T){
    ctx,cancel:=context.WithCancel(context.Background());defer cancel();polls:=0
    a:=fixture(func(_ context.Context,m string,_ json.RawMessage)(agentservice.Response,error){if m=="a2x.events"{polls++;cancel();return agentservice.Response{OK:true,Result:json.RawMessage(`{"events":[],"terminal":false}`)},nil};return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t"}}`)},nil})
    r:=httptest.NewRequest("POST",a.URL+"/rpc",strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"SubscribeToTask"}`)).WithContext(ctx);r.Header.Set("Content-Type","application/json");r.Header.Set("Authorization","Bearer private-grant");w:=httptest.NewRecorder();a.ServeHTTP(w,r);if polls!=1{t.Fatal(polls)}
}

func TestRPCTrailingSlash(t *testing.T){
    calls:=0;a:=fixture(func(context.Context,string,json.RawMessage)(agentservice.Response,error){calls++;return agentservice.Response{OK:true,Result:json.RawMessage(`{"tasks":[]}`)},nil})
    r:=httptest.NewRequest("POST",a.URL+"/rpc/",strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ListTasks","params":{}}`));r.Header.Set("Content-Type","application/json");r.Header.Set("Authorization","Bearer private-grant")
    w:=httptest.NewRecorder();a.ServeHTTP(w,r);if w.Code!=200||!json.Valid(w.Body.Bytes())||calls!=1{t.Fatal(w.Code,w.Body.String(),calls)}
}
func TestCardCacheRevalidatesGrant(t *testing.T){
    calls:=0;revoked:=false;name:="first"
    a:=fixture(func(context.Context,string,json.RawMessage)(agentservice.Response,error){calls++;if revoked{return agentservice.Response{Code:-32000,Error:"denied"},nil};data,_:=json.Marshal(map[string]string{"name":name});return agentservice.Response{OK:true,Result:data},nil})
    get:=func(token,tag string)*httptest.ResponseRecorder{r:=httptest.NewRequest("GET",a.URL+"/.well-known/agent-card.json",nil);r.Header.Set("Authorization","Bearer "+token);if tag!=""{r.Header.Set("If-None-Match",tag)};w:=httptest.NewRecorder();a.ServeHTTP(w,r);return w}
    first:=get("private-grant","");tag:=first.Header().Get("ETag")
    if first.Code!=200||tag==""||first.Header().Get("Cache-Control")!="private, max-age=0, must-revalidate"||first.Header().Get("Last-Modified")==""||!strings.Contains(first.Header().Get("Vary"),"Authorization"){t.Fatal(first.Code,first.Header())}
    for _,match:=range []string{tag,"W/"+tag,`"other", `+tag,"*"}{before:=calls;w:=get("private-grant",match);if w.Code!=304||w.Body.Len()!=0||calls!=before+1||w.Header().Get("ETag")!=tag{t.Fatal(w.Code,w.Body.String(),calls,before)}}
    before:=calls;if w:=get("wrong",tag);w.Code!=401||calls!=before{t.Fatal(w.Code,calls)}
    name="changed";changed:=get("private-grant",tag);if changed.Code!=200||changed.Header().Get("ETag")==tag{t.Fatal(changed.Code,changed.Header())}
    revoked=true;denied:=get("private-grant",changed.Header().Get("ETag"));if denied.Code==304||denied.Header().Get("ETag")!=""||denied.Header().Get("Cache-Control")!="no-store"{t.Fatal(denied.Code,denied.Header())}
}

func TestErrorInfoMatchesPinnedTCK(t *testing.T){
    for code,reason:=range map[int]string{-32001:"TASK_NOT_FOUND",-32002:"TASK_NOT_CANCELABLE",-32003:"PUSH_NOTIFICATION_NOT_SUPPORTED",-32004:"UNSUPPORTED_OPERATION",-32005:"CONTENT_TYPE_NOT_SUPPORTED",-32006:"INVALID_AGENT_RESPONSE",-32007:"EXTENDED_AGENT_CARD_NOT_CONFIGURED",-32008:"EXTENSION_SUPPORT_REQUIRED",-32009:"VERSION_NOT_SUPPORTED"}{
        data,_:=json.Marshal(failure(json.RawMessage(`1`),code));var value struct{Error struct{Data []map[string]string}};if err:=json.Unmarshal(data,&value);err!=nil{t.Fatal(err)}
        if len(value.Error.Data)!=1{t.Fatal(string(data))};entry:=value.Error.Data[0];if entry["@type"]!="type.googleapis.com/google.rpc.ErrorInfo"||entry["domain"]!="a2a-protocol.org"||entry["reason"]!=reason{t.Fatal(entry)}
    }
    if len(protocolError(-32602).Data)!=0{t.Fatal("standard JSONRPC error given fabricated A2A reason")}
}
func TestCardRepresentationTimestamp(t *testing.T){
    name:="first";a:=fixture(func(context.Context,string,json.RawMessage)(agentservice.Response,error){data,_:=json.Marshal(map[string]string{"name":name});return agentservice.Response{OK:true,Result:data},nil})
    get:=func()*httptest.ResponseRecorder{r:=httptest.NewRequest("GET",a.URL+"/.well-known/agent-card.json",nil);r.Header.Set("Authorization","Bearer private-grant");w:=httptest.NewRecorder();a.ServeHTTP(w,r);return w}
    before:=time.Now().UTC().Truncate(time.Second);first:=get();stamp,err:=http.ParseTime(first.Header().Get("Last-Modified"));if err!=nil||stamp.Before(before)||stamp.After(time.Now()){t.Fatal(stamp,err)}
    if next:=get();next.Header().Get("Last-Modified")!=first.Header().Get("Last-Modified"){t.Fatal("unchanged representation timestamp moved")}
    // Seed an older actual observation to test update without a timing sleep.
    a.cardMu.Lock();a.cardModified=before.Add(-time.Hour);a.cardMu.Unlock();name="changed"
    changed:=get();newStamp,err:=http.ParseTime(changed.Header().Get("Last-Modified"));if err!=nil||newStamp.Before(before)||changed.Header().Get("ETag")==first.Header().Get("ETag"){t.Fatal(changed.Header(),err)}
}
