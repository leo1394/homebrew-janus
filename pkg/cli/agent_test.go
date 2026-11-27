package cli

import (
    "bytes"
    "context"
    "encoding/json"
    "encoding/base64"
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
type agentFake struct{nativeTerminalTest bool;instances []janus.AgentInstance;discoveryError error;discoveries int;seenProfiles []janus.Profile;routeError error;infoName string;isolated bool;methods []string;params []json.RawMessage;mu sync.Mutex;terminal int;mfa bool;denyMFA bool;promptError bool;prompts int;infoType string;taskOnly bool;legacyInfo bool;infoInstance string;initializeType string;mfaTarget janus.AgentMFATarget}
func(f *agentFake)Ensure(context.Context,janus.Profile)error{return nil}
func(f *agentFake)Disconnect(context.Context,janus.Profile)error{return nil}
func(f *agentFake)Connected(context.Context,janus.Profile)(bool,error){return true,nil}
func(f *agentFake)Attach(context.Context,janus.Profile)error{f.terminal++;return nil}
func(f *agentFake)DeleteCredential(context.Context,janus.Profile)error{return nil}
func(f *agentFake)Agenux(ctx context.Context,p janus.Profile)(io.ReadWriteCloser,error){
    f.seenProfiles=append(f.seenProfiles,p);if f.routeError!=nil{return nil,f.routeError}
    local,remote:=net.Pipe()
    go func(){defer remote.Close();dec:=json.NewDecoder(remote);enc:=json.NewEncoder(remote)
        for {var m struct{ID json.RawMessage `json:"id"`;Method string `json:"method"`;Params json.RawMessage `json:"params"`};if dec.Decode(&m)!=nil{return}
            f.mu.Lock();f.methods=append(f.methods,m.Method);f.params=append(f.params,append(json.RawMessage(nil),m.Params...));f.mu.Unlock()
            result:=any(map[string]any{})
            if m.Method=="auth.verify" && f.denyMFA {enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"error":map[string]any{"code":-32000,"message":"fixture-code must not be logged"}});continue}
            if m.Method=="agent.info"&&f.legacyInfo{enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"error":map[string]any{"code":-32601,"message":"method not found"}});continue}
            switch m.Method {case "agent.info":kind:=f.infoType;if kind==""{kind=p.DeviceType()};instance:=f.infoInstance;if instance==""{instance="fixture-instance"};result=map[string]any{"instanceId":instance,"instanceName":f.infoName,"type":kind,"capabilities":map[string]bool{"delegatedTasks":f.taskOnly}};if f.isolated{result=map[string]any{"instanceId":instance,"type":kind,"principal":map[string]string{"id":"principal-fixture","username":"owner"},"capabilities":map[string]string{"workspaceIsolation":"principal-v1"}}};case "initialize":result=map[string]any{"mfaRequired":f.mfa,"terminal":!f.taskOnly,"type":f.initializeType};case "session.list":result=map[string]any{"sessions":[]map[string]string{{"id":"s1","name":"One","title":"Server title"}}};case "session.open","session.resume":result=map[string]string{"sessionId":"s1"};case "task.send":result=map[string]string{"text":"hello"}}
            if enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"result":result})!=nil{return}
            if m.Method=="terminal.open"&&f.nativeTerminalTest{
                enc.Encode(map[string]any{"jsonrpc":"2.0","method":"terminal.data","params":map[string]string{"sessionId":"s1","data":base64.StdEncoding.EncodeToString([]byte("ADAPTER_TUI_READY"))}})
                enc.Encode(map[string]any{"jsonrpc":"2.0","method":"terminal.exit","params":map[string]any{"sessionId":"s1","code":0}})
            }
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

func TestAgentSessionTitlesAndIdentity(t *testing.T){
    const first="6df70f07-7307-46dc-9902-62125b9fa27b"
    var sessions []a2x.Session
    if err:=json.Unmarshal([]byte(`[{"id":"`+first+`","name":"`+first+`","title":"修复数据库连接"},{"id":"other-session","name":"部署检查"},{"id":"unnamed-session","name":"unnamed-session"}]`),&sessions);err!=nil{t.Fatal(err)}
    got,err:=chooseAgentSession(sessions,first,func(rows []selector.Candidate)(selector.Candidate,bool,error){
        if rows[0].Name!="修复数据库连接"||rows[2].Name!="部署检查"||rows[3].Name!="Untitled session"{t.Fatalf("unreadable session labels: %+v",rows)}
        if rows[0].Path!=first{t.Fatal("display changed session identity")}
        return rows[0],true,nil
    })
    if err!=nil||got!=first{t.Fatal(got,err)}
}

func TestLastSessionBoundToReturnedScope(t *testing.T){
    rows:=[]a2x.Session{{ID:"current",Name:"Current"},{ID:"other",Name:"Other"}}
    if currentLastSession(rows,"foreign")!=""||currentLastSession(rows,"current")!="current"{t.Fatal("invalid last session scope")}
    _,err:=chooseAgentSession(rows,"foreign",func(candidates []selector.Candidate)(selector.Candidate,bool,error){if candidates[0].Path!=""{t.Fatal("foreign recency changed selection")};return candidates[0],true,nil});if err!=nil{t.Fatal(err)}
}

func TestSpawnAgentTerminalSelectionAndIdentity(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")}
    terminal,_,err:=s.Add("alice","example.com",2222);if err!=nil{t.Fatal(err)}
    other,_,err:=s.Add("bob","example.com",22);if err!=nil{t.Fatal(err)}
    agent,_,err:=s.AddAgent("alice","example.com",2222,"stdio");if err!=nil{t.Fatal(err)}
    rows,_:=s.List()
    selected,err:=chooseAgentTerminal(rows,func(candidates []selector.Candidate)(selector.Candidate,bool,error){
        if len(candidates)!=2{t.Fatal("non-terminal offered",candidates)}
        for _,c:=range candidates{if c.Path==displayID(agent.ID){t.Fatal("agent offered as terminal")};if c.Path==displayID(terminal.ID){return c,true,nil}}
        t.Fatal("terminal missing");return selector.Candidate{},false,nil
    });if err!=nil||selected.ID!=terminal.ID{t.Fatal(selected,err)}
    reused,pending,err:=s.PrepareAgentInstance(selected.User,selected.Host,selected.Port,"","","default");if err!=nil||pending||reused.ID!=agent.ID||reused.AgentTransport!="stdio"{t.Fatal(reused,pending,err)}
    fresh,pending,err:=s.PrepareAgentInstance(other.User,other.Host,other.Port,"","","default");if err!=nil||!pending||fresh.User!="bob"||fresh.Port!=22||fresh.AgentTransport!="subsystem"{t.Fatal(fresh,pending,err)}
    after,_:=s.List();if len(after)!=len(rows){t.Fatal("registered before authentication")}
    _,err=chooseAgentTerminal(rows,func([]selector.Candidate)(selector.Candidate,bool,error){return selector.Candidate{},false,nil});if err==nil{t.Fatal("cancel not honored")}
    _,err=chooseAgentTerminal([]janus.Profile{agent},func([]selector.Candidate)(selector.Candidate,bool,error){t.Fatal("picker opened without terminals");return selector.Candidate{},false,nil});if err==nil{t.Fatal("empty terminals accepted")}
}
func TestSpawnExistingAgentResumesInsteadOfOpening(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{}
    saved,_,err:=s.AddAgent("alice","example.com",7263,"stdio");if err!=nil{t.Fatal(err)}
    cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
    cmd.SetArgs([]string{"spawn","agent","alice@example.com"});cmd.SetIn(strings.NewReader("/exit\n"));cmd.SetOut(io.Discard);cmd.SetErr(io.Discard)
    if err:=cmd.Execute();err!=nil{t.Fatal(err)}
    r.mu.Lock();methods:=strings.Join(r.methods,",");r.mu.Unlock()
    if !strings.Contains(methods,"session.list")||!strings.Contains(methods,"session.resume")||strings.Contains(methods,"session.open"){t.Fatal(methods)}
    rows,_:=s.List();if len(rows)!=1||rows[0].ID!=saved.ID||rows[0].AgentTransport!="stdio"{t.Fatal(rows)}
}
func TestTerminalAgentKeepsCredentialReferenceAndRejectsAmbiguity(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")}
    terminal,_,err:=s.Add("alice","example.com",3233);if err!=nil{t.Fatal(err)}
    terminal.CredentialSlot=strings.Repeat("0123456789abcdef",4)
    if err=s.Save([]janus.Profile{terminal});err!=nil{t.Fatal(err)}
    fresh,pending,err:=s.PrepareAgentInstance(terminal.User,terminal.Host,terminal.Port,"","","default")
    if err!=nil||!pending||fresh.CredentialSlot!=terminal.CredentialSlot||fresh.Port!=3233{t.Fatal(fresh,pending,err)}
    _,_,err=s.AddAgentType("alice","example.com",3233,"stdio","pi");if err!=nil{t.Fatal(err)}
    _,_,err=s.AddAgentType("alice","example.com",3233,"subsystem","codex");if err!=nil{t.Fatal(err)}
    if _,_,err=s.PrepareAgentInstance(terminal.User,terminal.Host,terminal.Port,"","","default");err==nil{t.Fatal("ambiguous target selected")}
    if _,_,err=s.PrepareAgentInstance(terminal.User,terminal.Host,terminal.Port,"","pi","default");err==nil{t.Fatal("type selected an ambiguous instance")}
    changed:=terminal;changed.Port=22
    fresh,pending,err=s.PrepareAgentInstance(changed.User,changed.Host,changed.Port,"","","default");if err!=nil||!pending||fresh.CredentialSlot!=""{t.Fatal("different port reused credential reference",fresh,pending,err)}
}
func TestSpawnAgentByTerminalID(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{}
    terminal,_,err:=s.Add("alice","example.com",3233);if err!=nil{t.Fatal(err)}
    execute:=func(id string)error{
        cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{return err}
        cmd.SetArgs([]string{"spawn","agent",id});cmd.SetIn(strings.NewReader("/exit\n"));cmd.SetOut(io.Discard);cmd.SetErr(io.Discard);return cmd.Execute()
    }
    if err=execute(displayID(terminal.ID));err!=nil{t.Fatal(err)}
    rows,_:=s.List();var agent janus.Profile
    for _,p:=range rows{if p.IsAgent(){agent=p}}
    if len(rows)!=2||agent.User!=terminal.User||agent.Host!=terminal.Host||agent.Port!=3233||agent.AgentTransport!="subsystem"{t.Fatal(rows)}
    if err=execute(terminal.ID);err!=nil{t.Fatal(err)}
    rows,_=s.List();if len(rows)!=2{t.Fatal("duplicate agent",rows)}
    r.mu.Lock();methods:=strings.Join(r.methods,",");r.mu.Unlock()
    if strings.Count(methods,"session.open")!=1||strings.Count(methods,"session.resume")!=1{t.Fatal(methods)}
    if err=execute(displayID(agent.ID));err==nil||!strings.Contains(err.Error(),"terminal/ssh"){t.Fatal("agent ID accepted",err)}
    if err=execute("missing-device");err==nil||!strings.Contains(err.Error(),"not found"){t.Fatal(err)}
    rows=append(rows,janus.Profile{ID:"database:11111111-1111-4111-8111-111111111111",Platform:"database",Type:"mysql"})
    if _,err=terminalByReference(rows,"11111111-1111-4111-8111-111111111111");err==nil||!strings.Contains(err.Error(),"terminal/ssh"){t.Fatal("database ID accepted",err)}
}

func(f *agentFake)DiscoverAgentInstances(_ context.Context,p janus.Profile)([]janus.AgentInstance,error){
    f.discoveries++
    if f.discoveryError!=nil{return nil,f.discoveryError}
    if f.instances!=nil{return f.instances,nil}
    kind:=f.infoType;if kind==""{kind="pi"}
    return []janus.AgentInstance{{Name:"default",Type:kind,Status:"running"}},nil
}

func TestSpawnAgentByTerminalName(t *testing.T){
    for _,name:=range []string{"macOS-Runner","owner@friendly-name"}{t.Run(name,func(t *testing.T){
        s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};r:=&agentFake{}
        terminal,_,err:=s.Add("alice","example.com",3233);if err!=nil{t.Fatal(err)}
        if err:=s.Rename(terminal.ID,name);err!=nil{t.Fatal(err)}
        cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
        cmd.SetArgs([]string{"spawn","agent",strings.ToLower(name)});cmd.SetIn(strings.NewReader("/exit\n"));cmd.SetOut(io.Discard);cmd.SetErr(io.Discard)
        if err:=cmd.Execute();err!=nil{t.Fatal(err)}
        if len(r.seenProfiles)!=1||r.seenProfiles[0].User!="alice"||r.seenProfiles[0].Host!="example.com"||r.seenProfiles[0].Port!=3233{t.Fatal(r.seenProfiles)}
    })}
    rows:=[]janus.Profile{{ID:"terminal:one",Name:"Runner",Platform:"terminal",Type:"ssh"},{ID:"agent:two",Name:"Agent",Platform:"a2x",Type:"pi"},{ID:"database:three",Name:"DB",Platform:"database",Type:"mysql"}}
    for _,name:=range []string{"Agent","DB"}{if _,err:=terminalByReference(rows,name);err==nil||!strings.Contains(err.Error(),"terminal/ssh"){t.Fatal(name,err)}}
    rows=append(rows,janus.Profile{ID:"terminal:four",Name:"runner",Platform:"terminal",Type:"ssh"})
    if _,err:=terminalByReference(rows,"Runner");err==nil||!strings.Contains(err.Error(),"ambiguous"){t.Fatal(err)}
    if p,err:=terminalByReference(rows,"terminal:one");err!=nil||p.ID!="terminal:one"{t.Fatal(p,err)}
}
