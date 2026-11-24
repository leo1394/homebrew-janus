package a2x

import (
    "context"
    "encoding/json"
    "net"
    "testing"
    "time"
)
func TestConversationAndToolBoundary(t *testing.T) {
    local,remote:=net.Pipe()
    calls:=0
    c,err:=New(local,[]SharedTool{{Tool:Tool{Name:"approved",InputSchema:json.RawMessage(`{"type":"object"}`)},Call:func(ctx context.Context,call ToolCall)(ToolResult,error){calls++;return ToolResult{Content:[]Content{{Type:"text",Text:"local result"}}},nil}}},nil)
    if err!=nil{t.Fatal(err)};defer c.Close()
    done:=make(chan error,1)
    go func(){
        defer remote.Close();dec:=json.NewDecoder(remote);enc:=json.NewEncoder(remote)
        reply:=func(m message,result string)error{return enc.Encode(message{Version:"2.0",ID:m.ID,Result:json.RawMessage(result)})}
        var m message
        if err:=dec.Decode(&m);err!=nil{done<-err;return}
        if m.Method!="initialize"{t.Error(m.Method)}
        var init struct{Version int;Tools []Tool};json.Unmarshal(m.Params,&init)
        if init.Version!=1||len(init.Tools)!=1||init.Tools[0].Name!="approved"{t.Error("unexpected shared capabilities")}
        if err:=reply(m,`{}`);err!=nil{done<-err;return}
        if err:=dec.Decode(&m);err!=nil{done<-err;return}
        if m.Method!="session.open"{t.Error(m.Method)}
        if err:=reply(m,`{"sessionId":"s1"}`);err!=nil{done<-err;return}
        if err:=dec.Decode(&m);err!=nil{done<-err;return}
        if m.Method!="task.send"{t.Error(m.Method)}
        for _,test:=range []struct{name,session string;allowed bool}{{"hidden","s1",false},{"approved","other",false},{"approved","s1",true}}{
            params,_:=json.Marshal(ToolCall{SessionID:test.session,Name:test.name,Arguments:json.RawMessage(`{}`)})
            if err:=enc.Encode(message{Version:"2.0",ID:json.RawMessage(`"server-request"`),Method:"clientTool.call",Params:params});err!=nil{done<-err;return}
            var response message;if err:=dec.Decode(&response);err!=nil{done<-err;return}
            if (response.Error==nil)!=test.allowed {t.Error("tool authorization boundary",test,response)}
        }
        done<-reply(m,`{"text":"finished"}`)
    }()
    ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
    if err:=c.Initialize(ctx);err!=nil{t.Fatal(err)}
    if _,err:=c.Open(ctx,"");err!=nil{t.Fatal(err)}
    result,err:=c.Send(ctx,"hello");if err!=nil||result!="finished"{t.Fatal(result,err)}
    if err:=<-done;err!=nil{t.Fatal(err)}
    if calls!=1{t.Fatal("unshared or wrong-session tool executed",calls)}
}
func TestDisconnectUnblocksRequest(t *testing.T){
    local,remote:=net.Pipe();c,err:=New(local,nil,nil);if err!=nil{t.Fatal(err)};defer c.Close()
    go func(){var m message;json.NewDecoder(remote).Decode(&m);remote.Close()}()
    ctx,cancel:=context.WithTimeout(context.Background(),time.Second);defer cancel()
    if err:=c.Initialize(ctx);err==nil{t.Fatal("disconnect accepted")}
}
