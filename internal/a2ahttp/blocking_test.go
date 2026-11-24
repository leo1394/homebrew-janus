package a2ahttp

import (
    "context"
    "encoding/json"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/agentservice"
)
func TestBlockingSendUsesShortDelegatedCalls(t *testing.T){
    sends,gets:=0,0
    a:=fixture(func(_ context.Context,method string,params json.RawMessage)(agentservice.Response,error){
        var p map[string]json.RawMessage;json.Unmarshal(params,&p)
        switch method{
        case "SendMessage":
            sends++;var config map[string]json.RawMessage;json.Unmarshal(p["configuration"],&config)
            if string(config["returnImmediately"])!="true"{return agentservice.Response{Code:-32603,Error:"simulated M1 deadline exceeded by blocking SendMessage"},nil}
            if string(p["tenant"])!=`"scope-1"`||string(p["metadata"])!=`{"profile":"pi-report"}`||string(config["historyLength"])!="0"||string(config["acceptedOutputModes"])!=`["text/plain"]`{t.Fatal("submission changed parameters",string(params))}
            return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"}}}`)},nil
        case "GetTask":
            gets++;if string(p["tenant"])!=`"scope-1"`||string(p["historyLength"])!="0"||string(p["id"])!=`"t"`{t.Fatal("poll lost scope or history",string(params))}
            state:="TASK_STATE_WORKING";if gets==3{state="TASK_STATE_COMPLETED"}
            return agentservice.Response{OK:true,Result:json.RawMessage(`{"id":"t","status":{"state":"`+state+`"},"artifacts":[{"artifactId":"a"}]}`)},nil
        default:t.Fatal(method);return agentservice.Response{},nil
        }
    })
    w:=requestHTTP(a,`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"tenant":"scope-1","message":{"messageId":"same","parts":[{"text":"long task"}]},"metadata":{"profile":"pi-report"},"configuration":{"historyLength":0,"acceptedOutputModes":["text/plain"]}}}`,"private-grant")
    if sends!=1||gets!=3||!strings.Contains(w.Body.String(),"TASK_STATE_COMPLETED"){t.Fatal(sends,gets,w.Body.String())}
}

func TestBlockingSendModesAndFailures(t *testing.T){
    for _,state:=range []string{"TASK_STATE_COMPLETED","TASK_STATE_FAILED","TASK_STATE_CANCELED","TASK_STATE_REJECTED","TASK_STATE_INPUT_REQUIRED","TASK_STATE_AUTH_REQUIRED"}{
        t.Run(state,func(t *testing.T){calls:=0;a:=fixture(func(_ context.Context,m string,_ json.RawMessage)(agentservice.Response,error){calls++;result:=`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"}}}`;if m=="GetTask"{result=`{"id":"t","status":{"state":"`+state+`"}}`};return agentservice.Response{OK:true,Result:json.RawMessage(result)},nil})
            w:=requestHTTP(a,`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{}}`,"private-grant");if calls!=2||!strings.Contains(w.Body.String(),state){t.Fatal(calls,w.Body.String())}
        })
    }
    calls:=0;a:=fixture(func(_ context.Context,m string,_ json.RawMessage)(agentservice.Response,error){calls++;if m=="GetTask"{return agentservice.Response{Code:-32001,Error:"revoked secret"},nil};return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"}}}`)},nil})
    w:=requestHTTP(a,`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"configuration":{"returnImmediately":true}}}`,"private-grant");if calls!=1||!strings.Contains(w.Body.String(),"TASK_STATE_WORKING"){t.Fatal(calls,w.Body.String())}
    w=requestHTTP(a,`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{}}`,"private-grant");if calls!=3||!strings.Contains(w.Body.String(),`"code":-32001`)||strings.Contains(w.Body.String(),"secret"){t.Fatal(calls,w.Body.String())}
    ctx,cancel:=context.WithCancel(context.Background());polls:=0
    a=fixture(func(_ context.Context,m string,_ json.RawMessage)(agentservice.Response,error){if m=="GetTask"{polls++};cancel();return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"}}}`)},nil})
    _,code:=a.sendMessage(ctx,json.RawMessage(`{}`));if code==0||polls!=0{t.Fatal("cancel did not stop poll",code,polls)}
}

func TestPinnedTCKExecutionModeRequests(t *testing.T){
    for _,immediate:=range []bool{false,true}{
        sends,polls:=0,0;text:="Blocking request";if immediate{text="Non-blocking request"}
        a:=fixture(func(_ context.Context,method string,params json.RawMessage)(agentservice.Response,error){
            if method=="GetTask"{polls++;return agentservice.Response{OK:true,Result:json.RawMessage(`{"id":"t","status":{"state":"TASK_STATE_COMPLETED"}}`)},nil}
            sends++;var q struct{Message struct{MessageID,Role string;Parts []struct{Text string}};Configuration struct{ReturnImmediately bool}}
            if err:=json.Unmarshal(params,&q);err!=nil{t.Fatal(err)}
            if q.Message.MessageID!="complete-task"||q.Message.Role!="ROLE_USER"||len(q.Message.Parts)!=1||q.Message.Parts[0].Text!=text||!q.Configuration.ReturnImmediately{t.Fatal("changed official sample",string(params))}
            return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"}}}`)},nil
        })
        payload,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":map[string]any{"message":map[string]any{"role":"ROLE_USER","parts":[]map[string]string{{"text":text}},"messageId":"complete-task"},"configuration":map[string]bool{"returnImmediately":immediate}}})
        w:=requestHTTP(a,string(payload),"private-grant");state:="TASK_STATE_COMPLETED";expectedPolls:=1;if immediate{state="TASK_STATE_WORKING";expectedPolls=0}
        if sends!=1||polls!=expectedPolls||!strings.Contains(w.Body.String(),state){t.Fatal(immediate,sends,polls,w.Body.String())}
    }
}

func TestBlockingFinalDirectMessage(t *testing.T){
    final:=json.RawMessage(`{"id":"t","metadata":{"a2x.replyMode":"message"},"status":{"state":"TASK_STATE_COMPLETED","message":{"role":"ROLE_AGENT","messageId":"reply","contextId":"context","parts":[{"text":"answer"}]}}}`)
    for _,immediate:=range []bool{false,true}{
        calls:=0;a:=fixture(func(_ context.Context,m string,_ json.RawMessage)(agentservice.Response,error){calls++;if m=="SendMessage"{return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"}}}`)},nil};return agentservice.Response{OK:true,Result:final},nil})
        params:=json.RawMessage(`{}`);if immediate{params=json.RawMessage(`{"configuration":{"returnImmediately":true}}`)}
        got,code:=a.sendMessage(context.Background(),params);if code!=0{t.Fatal(code)}
        var result map[string]json.RawMessage;json.Unmarshal(got,&result)
        if immediate{if calls!=1||result["task"]==nil{t.Fatal(string(got),calls)}}else{if calls!=2||result["message"]==nil||result["task"]!=nil||!strings.Contains(string(result["message"]),"answer"){t.Fatal(string(got),calls)}}
    }
    a:=fixture(func(context.Context,string,json.RawMessage)(agentservice.Response,error){return agentservice.Response{OK:true,Result:json.RawMessage(`{"task":{"id":"t","metadata":{"a2x.replyMode":"message"},"status":{"state":"TASK_STATE_COMPLETED","message":{"role":"ROLE_USER"}}}}`)},nil})
    if _,code:=a.sendMessage(context.Background(),json.RawMessage(`{}`));code!=-32006{t.Fatal("invalid direct message accepted",code)}
}
