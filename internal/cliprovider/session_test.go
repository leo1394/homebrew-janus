package cliprovider

import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "github.com/leo1394/homebrew-janus/internal/database"
    "strings"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/store"
)
func TestConnectionReuseAndEOF(t *testing.T){
    dir,err:=os.MkdirTemp("/private/tmp","jcli-");if err!=nil{t.Fatal(err)};defer os.RemoveAll(dir)
    s:=store.Store{Root:dir};a,err:=s.PrepareCLIAccount("fixture","test");if err!=nil{t.Fatal(err)};if err:=s.RegisterCLIAccount(a);err!=nil{t.Fatal(err)}
    input,writer:=io.Pipe();reader,output:=io.Pipe();defer reader.Close();defer writer.Close()
    calls:=0;logouts:=0
    factory:=func(a store.Account)*Client{c:=New(a);c.http.Transport=transport(func(r *http.Request)(*http.Response,error){body:=`{"code":0,"data":{}}`;switch r.URL.Path{case "/api/v1/auth/login":body=`{"code":0,"data":{"accessToken":"private-access","refreshToken":"private-refresh","userId":"u1","expiresIn":600}}`;case "/api/v1/auth/me":body=`{"code":0,"data":{"id":"u1","username":"fixture"}}`;case "/api/v1/auth/logout":logouts++};return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(body)),Header:make(http.Header)},nil});return c}
    done:=make(chan error,1);go func(){defer output.Close();done<-serve(context.Background(),s,"",input,output,func(context.Context,store.Account,string,bool,...string)([]byte,error){calls++;return []byte("synthetic"),nil},factory)}()
    decoder:=json.NewDecoder(reader)
    call:=func(method string,parameters map[string]string)map[string]any{t.Helper();if err:=json.NewEncoder(writer).Encode(request{Version:1,ID:"q",Method:method,Parameters:parameters});err!=nil{t.Fatal(err)};var result struct{Response map[string]any `json:"response"`};if err:=decoder.Decode(&result);err!=nil{t.Fatal(err)};wire,_:=json.Marshal(result);if strings.Contains(string(wire),"private-"){t.Fatal("token escaped")};return result.Response}
    opened:=call("session.request",map[string]string{"resourceID":a.ID});id,ok:=opened["sessionID"].(string);if !ok{t.Fatal(opened)}
    for i:=0;i<2;i++{result:=call("session.execute",map[string]string{"sessionID":id,"operation":"inspect","subset":"identity"});if result["result"]==nil{t.Fatal(result)}}
    denied:=call("session.execute",map[string]string{"sessionID":id,"operation":"exec","subset":"identity"});if denied["error"]==nil{t.Fatal("arbitrary operation allowed")}
    writer.Close();select{case err:=<-done:if err!=nil{t.Fatal(err)};case <-time.After(3*time.Second):t.Fatal("EOF cleanup stalled")};if calls!=1||logouts!=1{t.Fatal("approval reuse or logout failed",calls,logouts)}
}

func TestRevokeAcknowledgmentBeforeShutdown(t *testing.T){
    dir,err:=os.MkdirTemp("/private/tmp","jcli-");if err!=nil{t.Fatal(err)};defer os.RemoveAll(dir)
    s:=store.Store{Root:dir};a,err:=s.PrepareCLIAccount("fixture","test");if err!=nil{t.Fatal(err)};if err:=s.RegisterCLIAccount(a);err!=nil{t.Fatal(err)}
    input,writer:=io.Pipe();reader,output:=io.Pipe();defer reader.Close();defer writer.Close()
    calls:=0;logouts:=0
    factory:=func(a store.Account)*Client{c:=New(a);c.http.Transport=transport(func(r *http.Request)(*http.Response,error){body:=`{"code":0,"data":{}}`;switch r.URL.Path{case "/api/v1/auth/login":body=`{"code":0,"data":{"accessToken":"private-access","refreshToken":"private-refresh","userId":"u1","expiresIn":600}}`;case "/api/v1/auth/me":body=`{"code":0,"data":{"id":"u1","username":"fixture"}}`;case "/api/v1/auth/logout":logouts++};return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(body)),Header:make(http.Header)},nil});return c}
    done:=make(chan error,1);go func(){defer output.Close();done<-serve(context.Background(),s,"",input,output,func(context.Context,store.Account,string,bool,...string)([]byte,error){calls++;return []byte("synthetic"),nil},factory)}()
    decoder:=json.NewDecoder(reader)
    call:=func(method string,parameters map[string]string)map[string]any{t.Helper();if err:=json.NewEncoder(writer).Encode(request{Version:1,ID:"q",Method:method,Parameters:parameters});err!=nil{t.Fatal(err)};var result struct{Response map[string]any `json:"response"`};if err:=decoder.Decode(&result);err!=nil{t.Fatal(err)};wire,_:=json.Marshal(result);if strings.Contains(string(wire),"private-"){t.Fatal("token escaped")};return result.Response}
    opened:=call("session.request",map[string]string{"resourceID":a.ID});id,ok:=opened["sessionID"].(string);if !ok{t.Fatal(opened)}
    for i:=0;i<2;i++{result:=call("session.execute",map[string]string{"sessionID":id,"operation":"inspect","subset":"identity"});if result["result"]==nil{t.Fatal(result)}}
    denied:=call("session.execute",map[string]string{"sessionID":id,"operation":"exec","subset":"identity"});if denied["error"]==nil{t.Fatal("arbitrary operation allowed")}
    paths,_:=filepath.Glob(filepath.Join(dir,"cli","*.sock"));if len(paths)!=1{t.Fatal("missing control socket")}
    control,err:=database.Dial(context.Background(),paths[0]);if err!=nil{t.Fatal(err)};defer control.Close()
    if reply,err:=control.Call(context.Background(),database.Request{Method:"list"});err!=nil||len(reply.Sessions)!=1{t.Fatal("list before revoke failed",err)}
    if _,err:=control.Call(context.Background(),database.Request{Method:"revoke",SessionID:id});err!=nil{t.Fatal("revoke acknowledgment lost",err)}
    select{case err:=<-done:if err!=nil{t.Fatal(err)};case <-time.After(3*time.Second):t.Fatal("EOF cleanup stalled")};if calls!=1||logouts!=1{t.Fatal("approval reuse or logout failed",calls,logouts)}
}

func TestIdleTimeoutBoundsBeforeAuthorization(t *testing.T){
    for _,duration:=range []time.Duration{0,-time.Minute,time.Second,5*time.Minute-time.Second,time.Hour+time.Second}{
        if err:=ServeWithIdleTimeout(context.Background(),store.Store{},"",strings.NewReader(""),io.Discard,duration);err==nil{t.Fatal("invalid timeout accepted",duration)}
    }
    for _,duration:=range []time.Duration{5*time.Minute,15*time.Minute,time.Hour}{
        if err:=ServeWithIdleTimeout(context.Background(),store.Store{},"",strings.NewReader(""),io.Discard,duration);err!=nil{t.Fatal("valid timeout rejected",duration,err)}
    }
}
