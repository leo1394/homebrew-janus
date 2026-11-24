package cli

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "net"
    "path/filepath"
    "strings"
    "sync"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
type agentFake struct{methods []string;params []json.RawMessage;mu sync.Mutex;terminal int;mfa bool;denyMFA bool;promptError bool;prompts int;infoType string;taskOnly bool;legacyInfo bool;infoInstance string;initializeType string;mfaTarget janus.AgentMFATarget}
func(f *agentFake)Ensure(context.Context,janus.Profile)error{return nil}
func(f *agentFake)Disconnect(context.Context,janus.Profile)error{return nil}
func(f *agentFake)Connected(context.Context,janus.Profile)(bool,error){return true,nil}
func(f *agentFake)Attach(context.Context,janus.Profile)error{f.terminal++;return nil}
func(f *agentFake)DeleteCredential(context.Context,janus.Profile)error{return nil}
func(f *agentFake)Agenux(ctx context.Context,p janus.Profile)(io.ReadWriteCloser,error){
    local,remote:=net.Pipe()
    go func(){defer remote.Close();dec:=json.NewDecoder(remote);enc:=json.NewEncoder(remote)
        for {var m struct{ID json.RawMessage `json:"id"`;Method string `json:"method"`;Params json.RawMessage `json:"params"`};if dec.Decode(&m)!=nil{return}
            f.mu.Lock();f.methods=append(f.methods,m.Method);f.params=append(f.params,append(json.RawMessage(nil),m.Params...));f.mu.Unlock()
            result:=any(map[string]any{})
            if m.Method=="auth.verify" && f.denyMFA {enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"error":map[string]any{"code":-32000,"message":"fixture-code must not be logged"}});continue}
            if m.Method=="agent.info"&&f.legacyInfo{enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"error":map[string]any{"code":-32601,"message":"method not found"}});continue}
            switch m.Method {case "agent.info":kind:=f.infoType;if kind==""{kind=p.DeviceType()};instance:=f.infoInstance;if instance==""{instance="fixture-instance"};result=map[string]any{"instanceId":instance,"type":kind,"capabilities":map[string]bool{"delegatedTasks":f.taskOnly}};case "initialize":result=map[string]any{"mfaRequired":f.mfa,"terminal":!f.taskOnly,"type":f.initializeType};case "session.list":result=map[string]any{"sessions":[]map[string]string{{"id":"s1","name":"One"}}};case "session.open","session.resume":result=map[string]string{"sessionId":"s1"};case "task.send":result=map[string]string{"text":"hello"}}
            if enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"result":result})!=nil{return}
        }
    }()
    return local,nil
}
func TestAgentCommandsAndDispatch(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{}
    execute:=func(args ...string)string{t.Helper();cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};var out bytes.Buffer;cmd.SetIn(strings.NewReader("hello\n/exit\n"));cmd.SetOut(&out);cmd.SetErr(io.Discard);cmd.SetArgs(args);if err:=cmd.Execute();err!=nil{t.Fatal(args,err)};return out.String()}
    if out:=execute("spawn","agent","alice@example.com","-p","7263");!strings.Contains(out,"hello"){t.Fatal(out)}
    rows,err:=s.List();if err!=nil||len(rows)!=1||!rows[0].IsAgent()||rows[0].LastSession!="s1"{t.Fatal(rows,err)}
    execute("connect",rows[0].Name)
    execute("connect",rows[0].Name,"--empty")
    r.mu.Lock();methods:=strings.Join(r.methods,",");r.mu.Unlock()
    if strings.Count(methods,"session.open")!=2||strings.Count(methods,"session.resume")!=1{t.Fatal(methods)}
    terminal,_,err:=s.Add("bob","example.com",22);if err!=nil{t.Fatal(err)};execute("connect",terminal.Name)
    if r.terminal!=1{t.Fatal("terminal adapter not selected")}
    if out:=execute("devices","--json");!strings.Contains(out,`"platform":"a2x"`)||!strings.Contains(out,`"type":"pi"`){t.Fatal(out)}
}

func TestMultipleSessionsAlwaysRequireSelection(t *testing.T){
    sessions:=[]a2x.Session{{ID:"s1",Name:"One"},{ID:"s2",Name:"Two"}}
    for _,last:=range []string{"s1","s2","missing",""}{
        for _,choice:=range []string{"","s1","s2"}{
            called:=false
            got,err:=chooseAgentSession(sessions,last,func(rows []selector.Candidate)(selector.Candidate,bool,error){
                called=true
                if len(rows)!=3{t.Fatal(rows)}
                if (last=="s1"||last=="s2")&&rows[0].Path!=last{t.Fatal("recent session not initially focused",rows)}
                for _,row:=range rows{if row.Path==choice{return row,true,nil}}
                t.Fatal("missing new or existing session",choice);return selector.Candidate{},false,nil
            })
            if err!=nil||!called||got!=choice{t.Fatal(last,choice,got,err,called)}
        }
    }
    if _,err:=chooseAgentSession(sessions,"s1",func([]selector.Candidate)(selector.Candidate,bool,error){return selector.Candidate{},false,nil});err==nil{t.Fatal("cancel resumed last session")}
    for _,rows:=range [][]a2x.Session{nil,sessions[:1]}{
        got,err:=chooseAgentSession(rows,"s2",func([]selector.Candidate)(selector.Candidate,bool,error){t.Fatal("unnecessary picker");return selector.Candidate{},false,nil})
        expected:="";if len(rows)==1{expected="s1"};if err!=nil||got!=expected{t.Fatal(got,err)}
    }
}

func(f *agentFake)AgentMFA(_ context.Context,target janus.AgentMFATarget)([]byte,error){f.mfaTarget=target;f.prompts++;if f.promptError{return nil,errors.New("cancelled")};return []byte("fixture-code"),nil}
func TestAgentMFABeforeSession(t *testing.T){
    for _,tc:=range []struct{name string;denied,cancelled bool}{{"success",false,false},{"server denied",true,false},{"prompt cancelled",false,true}}{
        t.Run(tc.name,func(t *testing.T){
            s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{mfa:true,denyMFA:tc.denied,promptError:tc.cancelled}
            cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};cmd.SetArgs([]string{"spawn","agent","alice@example.com"});cmd.SetIn(strings.NewReader("/exit\n"));var output bytes.Buffer;cmd.SetOut(&output);cmd.SetErr(&output)
            err=cmd.Execute();if (err!=nil)!=(tc.denied||tc.cancelled){t.Fatal(err)}
            if r.prompts!=1{t.Fatal("missing MFA prompt")}
            r.mu.Lock();methods:=strings.Join(r.methods,",");r.mu.Unlock()
            if tc.denied||tc.cancelled{if strings.Contains(methods,"session."){t.Fatal("session opened before MFA",methods)}}else if methods!="initialize,auth.verify,agent.info,session.open"{t.Fatal(methods)}
            if strings.Contains(output.String(),"fixture-code")||(err!=nil&&strings.Contains(err.Error(),"fixture-code")){t.Fatal("MFA code exposed")}
        })
    }
}

func TestCodexDeviceAdapter(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{}
    p,_,err:=s.AddAgentType("alice","example.com",7263,"subsystem","codex");if err!=nil{t.Fatal(err)}
    cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};cmd.SetArgs([]string{"connect",p.ID,"--empty"});cmd.SetIn(strings.NewReader("/exit\n"));cmd.SetOut(io.Discard);cmd.SetErr(io.Discard)
    if err:=cmd.Execute();err!=nil{t.Fatal(err)};got,err:=s.Get(p.ID);if err!=nil||got.DeviceType()!="codex"||got.LastSession!="s1"{t.Fatal(got,err)}
}

func TestDeploymentNamedDeviceAdapter(t *testing.T){
    for _,kind:=range []string{"opencode","claude-code","research-acp"}{
        s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{}
        p,_,err:=s.AddAgentType("alice","example.com",7263,"subsystem",kind);if err!=nil{t.Fatal(err)}
        cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};cmd.SetArgs([]string{"connect",p.ID,"--empty"});cmd.SetIn(strings.NewReader("/exit\n"));cmd.SetOut(io.Discard);cmd.SetErr(io.Discard)
        if err:=cmd.Execute();err!=nil{t.Fatal(kind,err)};got,err:=s.Get(p.ID);if err!=nil||got.DeviceType()!=kind||got.DevicePlatform()!="a2x"||got.LastSession!="s1"{t.Fatal(got,err)}
    }
}

func(f *agentFake)AgenuxPending(ctx context.Context,p janus.Profile)(io.ReadWriteCloser,error){return f.Agenux(ctx,p)}

func TestAgentMFADisplayContext(t *testing.T){
    for _,tc:=range []struct{name,advertised,boundName,selected,boundID,want string}{
        {"advertised named","opencode","","test","default-id","opencode"},
        {"transient unknown","","","test","default-id",""},
        {"bound named","","test","test","named-id","codex"},
        {"bound default","","","","default-id","codex"},
        {"new unknown","","","","",""},
        {"invalid display","pi\nInjected","","test","default-id",""},
    }{t.Run(tc.name,func(t *testing.T){
        p:=janus.Profile{User:"owner",Host:"example.com",Port:7263,Platform:"a2x",Type:"codex",InstanceName:tc.boundName,InstanceID:tc.boundID}
        r:=&agentFake{mfa:true,initializeType:tc.advertised};stream,err:=r.Agenux(context.Background(),p);if err!=nil{t.Fatal(err)}
        client,err:=a2x.New(stream,nil,nil);if err!=nil{t.Fatal(err)};defer client.Close()
        if err:=authenticateAgent(context.Background(),client,r,p,tc.selected);err!=nil{t.Fatal(err)}
        if r.mfaTarget.Target!="owner@example.com:7263"||r.mfaTarget.Type!=tc.want||r.mfaTarget.InstanceName!=tc.selected||r.prompts!=1{t.Fatal(r.mfaTarget,r.prompts)}
    })}
}
