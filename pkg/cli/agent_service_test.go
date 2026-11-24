package cli

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/agentservice"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
type serviceFixture struct{agentFake;opened int;lock sync.Mutex;remote net.Conn;instanceName string;longDuration time.Duration}
func(f *serviceFixture)Agenux(ctx context.Context,p janus.Profile)(io.ReadWriteCloser,error){
    left,right:=net.Pipe();f.lock.Lock();f.opened++;f.remote=right;f.lock.Unlock()
    go func(){
        defer right.Close();dec:=json.NewDecoder(right);enc:=json.NewEncoder(right)
        grants:=map[string]string{};tasks:=map[string]map[string]any{};keys:=map[string]string{};next:=0;authorized:=false;var longUntil time.Time
        for {var m struct{ID json.RawMessage;Method string;Params json.RawMessage};if dec.Decode(&m)!=nil{return}
            result:=any(map[string]any{});failure:=""
            switch m.Method{
            case "initialize":result=map[string]any{"mfaRequired":true}
            case "auth.verify":authorized=true
            default:if !authorized{failure="MFA required"}
            }
            if failure==""{switch m.Method{
            case "agent.info":result=map[string]string{"instanceId":"instance-test","type":f.infoType}
            case "grant.create":next++;id:=fmt.Sprint(next);grants[id]="secret-"+id;result=map[string]string{"grantId":id,"credential":grants[id],"instanceId":"instance-test"}
            case "grant.revoke":var p struct{GrantID string};json.Unmarshal(m.Params,&p);delete(grants,p.GrantID)
            case "delegated.call":
                var p struct{GrantID,Credential,Method string;Params json.RawMessage};json.Unmarshal(m.Params,&p)
                if grants[p.GrantID]==""||grants[p.GrantID]!=p.Credential{failure="credential rejected: "+p.Credential;break}
                switch p.Method{
                case "agent.card","GetExtendedAgentCard":result=map[string]any{"name":"fixture","supportedInterfaces":[]any{map[string]string{"url":"ssh://remote"}}}
                case "SendMessage":
                    state:="TASK_STATE_COMPLETED"
                    if f.longDuration>0{
                        var q struct{Configuration struct{ReturnImmediately bool}};json.Unmarshal(p.Params,&q)
                        if !q.Configuration.ReturnImmediately{time.Sleep(f.longDuration)}else{longUntil=time.Now().Add(f.longDuration);state="TASK_STATE_WORKING"}
                    }
                    result=map[string]any{"task":map[string]any{"id":"a2a-task","status":map[string]string{"state":state}}}
                case "GetTask":state:="TASK_STATE_COMPLETED";if time.Now().Before(longUntil){state="TASK_STATE_WORKING"};result=map[string]any{"id":"a2a-task","status":map[string]string{"state":state}}
                case "SendStreamingMessage","SubscribeToTask":result=map[string]any{"task":map[string]string{"id":"a2a-task"},"cursor":2}
                case "a2x.events":result=map[string]any{"events":[]any{map[string]any{"seq":3,"event":map[string]any{"statusUpdate":map[string]any{"taskId":"a2a-task","status":map[string]string{"state":"TASK_STATE_COMPLETED"}}}}},"terminal":true}
                case "CreateTaskPushNotificationConfig":failure="A2A -32003 secret-1 should not leak"
                case "agent.status":result=map[string]string{"state":"ready"}
                case "tasks.submit":
                    var q struct{IdempotencyKey,Text string};json.Unmarshal(p.Params,&q);key:=p.GrantID+":"+q.IdempotencyKey;id:=keys[key];if id==""{id=fmt.Sprintf("task-%d",len(tasks)+1);keys[key]=id;tasks[id]=map[string]any{"taskId":id,"state":"completed","artifact":map[string]string{"name":"report.md","mimeType":"text/markdown","text":q.Text,"sha256":"fixture"},"usage":nil,"owner":p.GrantID}};result=tasks[id]
                case "tasks.get":var q struct{TaskID string};json.Unmarshal(p.Params,&q);if tasks[q.TaskID]==nil||tasks[q.TaskID]["owner"]!=p.GrantID{failure="not owned"}else{result=tasks[q.TaskID]}
                case "tasks.list":list:=[]any{};for _,task:=range tasks{if task["owner"]==p.GrantID{list=append(list,task)}};result=map[string]any{"tasks":list}
                default:failure="not allowed"
                }
            case "tasks.list":list:=[]any{};for _,task:=range tasks{list=append(list,task)};result=map[string]any{"tasks":list}
            case "tasks.get":var q struct{TaskID string};json.Unmarshal(m.Params,&q);result=tasks[q.TaskID]
            }}
            response:=map[string]any{"jsonrpc":"2.0","id":m.ID,"result":result};if failure!=""{delete(response,"result");response["error"]=map[string]any{"code":-32000,"message":failure}}
            if enc.Encode(response)!=nil{return}
        }
    }()
    return left,nil
}
func(f *serviceFixture)AgenuxInstance(ctx context.Context,p janus.Profile,name string)(io.ReadWriteCloser,error){f.lock.Lock();f.instanceName=name;f.lock.Unlock();return f.Agenux(ctx,p)}
type serviceReadyWriter struct{once sync.Once;ready chan struct{}}
func(w *serviceReadyWriter)Write(p []byte)(int,error){if bytes.Contains(p,[]byte(`"ready":true`)){w.once.Do(func(){close(w.ready)})};return len(p),nil}
type httpReadyWriter struct{url chan string}
func(w *httpReadyWriter)Write(p []byte)(int,error){var value struct{Ready bool;URL string};if json.Unmarshal(p,&value)==nil&&value.Ready{w.url<-value.URL};return len(p),nil}
func TestAgentServiceLocalEndToEnd(t *testing.T){
    for _,mode:=range []int{0,1,2}{t.Run(fmt.Sprint(mode),func(t *testing.T){
    named:=mode>0
    // Short private path also fits macOS's 104-byte Unix socket path limit.
    base,err:=os.MkdirTemp("/private/tmp","janus-m1-");if err!=nil{t.Fatal(err)};defer os.RemoveAll(base)
    s:=store.Store{Root:filepath.Join(base,"registry")};p,_,err:=s.AddAgent("owner","fixture.invalid",7263,"subsystem");if err!=nil{t.Fatal(err)}
    p.Platform="ai";p.Type="agent";if named{p.AgentTransport="stdio";p.InstanceID="default-instance"};if err:=s.Save([]janus.Profile{p});err!=nil{t.Fatal(err)}
    r:=&serviceFixture{};if mode==2{r.infoType="opencode"};if !named&&os.Getenv("JANUS_LONG_TASK_TEST")=="1"{r.longDuration=41*time.Second};service,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
    directory:=filepath.Join(base,"connection");ready:=&serviceReadyWriter{ready:make(chan struct{})};service.SetOut(ready);service.SetErr(io.Discard);service.SetArgs([]string{"agent","service",p.Name,"--directory",directory})
    if named{service.SetArgs([]string{"agent","service",p.Name,"--directory",directory,"--instance","m1-test"})}
    if mode==2{service.SetArgs([]string{"agent","service",p.Name,"--directory",directory,"--instance","m1-test","--register-instance"})}
    ctx,cancel:=context.WithCancel(context.Background());defer cancel();service.SetContext(ctx);done:=make(chan error,1);go func(){done<-service.Execute()}()
    select{case <-ready.ready:case err:=<-done:t.Fatal(err);case <-time.After(5*time.Second):t.Fatal("service startup timeout")}
    run:=func(input string,args ...string)(agentservice.Response,error){t.Helper();cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};var out bytes.Buffer;cmd.SetIn(strings.NewReader(input));cmd.SetOut(&out);cmd.SetErr(io.Discard);cmd.SetArgs(args);err=cmd.Execute();if strings.Contains(out.String(),"secret-"){t.Fatal("credential leaked to stdout")};var response agentservice.Response;if e:=json.Unmarshal(out.Bytes(),&response);e!=nil{t.Fatal(out.String(),e)};return response,err}
    credentialPath:=filepath.Join(base,"actor.json")
    created,err:=run("","agent","grant","create","--directory",directory,"--actor-id","reporter","--credential-out",credentialPath);if err!=nil||!created.OK{t.Fatal(created,err)}
    credential,err:=agentservice.ReadCredential(credentialPath);if err!=nil{t.Fatal(err)}
    httpCommand,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
    httpCtx,httpCancel:=context.WithCancel(ctx);defer httpCancel();httpCommand.SetContext(httpCtx)
    httpReady:=&httpReadyWriter{url:make(chan string,1)};httpCommand.SetOut(httpReady);httpCommand.SetErr(io.Discard);httpCommand.SetArgs([]string{"agent","a2a","--directory",directory,"--credential-file",credentialPath})
    httpDone:=make(chan error,1);go func(){httpDone<-httpCommand.Execute()}()
    var httpURL string;select{case httpURL=<-httpReady.url:case err:=<-httpDone:t.Fatal(err);case <-time.After(5*time.Second):t.Fatal("HTTP adapter not ready")}
    httpCall:=func(method string)string{t.Helper();body,_:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":1,"method":method,"params":map[string]any{}});req,_:=http.NewRequest("POST",httpURL,bytes.NewReader(body));req.Header.Set("Authorization","Bearer "+credential.Credential);req.Header.Set("Content-Type","application/json");response,err:=(&http.Client{Timeout:5*time.Second+r.longDuration}).Do(req);if err!=nil{t.Fatal(err)};defer response.Body.Close();data,err:=io.ReadAll(response.Body);if err!=nil{t.Fatal(err)};if bytes.Contains(data,[]byte(credential.Credential)){t.Fatal("HTTP credential leaked")};return string(data)}
    ownerProbe:=make(chan error,1)
    if r.longDuration>0{go func(){time.Sleep(500*time.Millisecond);probe,stop:=context.WithTimeout(ctx,2*time.Second);defer stop();reply,err:=agentservice.Call(probe,directory,true,agentservice.Request{Method:"tasks.list",Params:json.RawMessage(`{}`)});if err==nil&&!reply.OK{err=fmt.Errorf("owner query rejected")};ownerProbe<-err}()}
    started:=time.Now()
    if body:=httpCall("SendMessage");!strings.Contains(body,"TASK_STATE_COMPLETED"){t.Fatal(body)}
    if r.longDuration>0{if time.Since(started)<r.longDuration{t.Fatal("returned before long task completed")};if err:=<-ownerProbe;err!=nil{t.Fatal("long wait blocked owner query",err)}}
    if body:=httpCall("SendStreamingMessage");!strings.Contains(body,"data: ")||!strings.Contains(body,"TASK_STATE_COMPLETED")||strings.Contains(body,"cursor"){t.Fatal(body)}
    if body:=httpCall("CreateTaskPushNotificationConfig");!strings.Contains(body,`"code":-32003`){t.Fatal(body)}
    delegated:=func(method,params string)(agentservice.Response,error){return run(params,"agent","delegated","--directory",directory,"--credential-file",credentialPath,"--method",method)}
    submitted,err:=delegated("tasks.submit",`{"idempotencyKey":"one","text":"report","profile":"pi-report"}`);if err!=nil{t.Fatal(err)}
    again,err:=delegated("tasks.submit",`{"idempotencyKey":"one","text":"report","profile":"pi-report"}`);if err!=nil||!bytes.Equal(again.Result,submitted.Result){t.Fatal("idempotency lost",again,err)}
    var task struct{TaskID,State string};json.Unmarshal(submitted.Result,&task);if task.State!="completed"{t.Fatal(task)}
    for _,method:=range []string{"tasks.list","agent.status"}{if _,err:=delegated(method,`{}`);err!=nil{t.Fatal(err)}}
    if _,err:=delegated("tasks.get",`{"taskId":"`+task.TaskID+`"}`);err!=nil{t.Fatal(err)}
    for _,method:=range []string{"terminal.open","grant.create","message/send","session.open"}{if _,err:=delegated(method,`{}`);err==nil{t.Fatal("delegated privileged method allowed",method)}}
    for _,method:=range []string{"get","list"}{args:=[]string{"agent","tasks",method};if method=="get"{args=append(args,task.TaskID)};args=append(args,"--directory",directory);if _,err:=run("",args...);err!=nil{t.Fatal(err)}}
    q:=agentservice.Request{InstanceID:"other-instance",GrantID:credential.GrantID,Credential:credential.Credential,Method:"agent.status",Params:json.RawMessage(`{}`)}
    rejected,err:=agentservice.Call(ctx,directory,false,q);if err!=nil||rejected.OK{t.Fatal("instance mismatch accepted",rejected,err)}
    q.InstanceID="instance-test";q.Method="terminal.open"
    rejected,err=agentservice.Call(ctx,directory,false,q);if err!=nil||rejected.OK{t.Fatal("direct delegated endpoint allowed terminal",rejected,err)}
    if _,err:=run("","agent","grant","revoke",credential.GrantID,"--directory",directory);err!=nil{t.Fatal(err)}
    rejected,err=delegated("agent.status",`{}`);if err==nil||rejected.OK{t.Fatal("revoked grant accepted")}
    if body:=httpCall("SendMessage");!strings.Contains(body,`"error"`){t.Fatal("HTTP bypassed revoked grant",body)}
    httpCancel();select{case err:=<-httpDone:if err!=nil{t.Fatal(err)};case <-time.After(5*time.Second):t.Fatal("HTTP adapter did not stop")}
    got,err:=s.Get(p.ID);if err!=nil||got.ID!=p.ID{t.Fatal(got,err)}
    if named{if got.InstanceID!="default-instance"||got.Platform!="ai"||got.Type!="agent"{t.Fatal("named instance mutated default binding",got)}}else if got.InstanceID!="instance-test"||got.Platform!="a2x"||got.Type!="pi"{t.Fatal(got)}
    if mode==2{rows,err:=s.List();if err!=nil||len(rows)!=2{t.Fatal(rows,err)};var saved janus.Profile;for _,row:=range rows{if row.ID!=p.ID{saved=row}};if saved.InstanceName!="m1-test"||saved.InstanceID!="instance-test"||saved.Type!="opencode"||saved.Platform!="a2x"{t.Fatal(saved)}}
    r.lock.Lock();opened:=r.opened;r.lock.Unlock();if opened!=1||r.prompts!=1{t.Fatal("did not retain authentication",opened,r.prompts)}
    // Transport EOF must terminate the listener, never silently reconnect.
    r.lock.Lock();r.remote.Close();r.lock.Unlock()
    select{case err:=<-done:if err==nil{t.Fatal("lost connection reported success")};case <-time.After(5*time.Second):t.Fatal("service did not stop on EOF")}
    if _,err:=agentservice.Call(ctx,directory,false,q);err==nil{t.Fatal("listener survived remote EOF")}
    })}
}
