package agentservice

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
)
func TestDelegationMethodBoundary(t *testing.T){
    for _,method:=range []string{"terminal.open","terminal.input","session.open","auth.verify","grant.create","grant.revoke","message/send","delegated.call"}{
        if Validate(Request{GrantID:"g",Credential:"c",Method:method,Params:json.RawMessage(`{}`)},false)==nil{t.Fatal(method)}
    }
    for _,params:=range []string{`{"idempotencyKey":"k","text":"x","profile":"shell;sh"}`,`{"idempotencyKey":"k","text":"x","profile":"pi-report","command":"sh"}`,`{"idempotencyKey":"","text":"x","profile":"pi-report"}`}{
        if Validate(Request{GrantID:"g",Credential:"c",Method:"tasks.submit",Params:json.RawMessage(params)},false)==nil{t.Fatal(params)}
    }
    if Validate(Request{Method:"tasks.submit",Params:json.RawMessage(`{"idempotencyKey":"k","text":"x","profile":"pi-report"}`)},true)==nil{t.Fatal("owner submission permitted")}
    if Validate(Request{Method:"grant.create",Params:json.RawMessage(`{"actorId":"a","profile":"pi-report","ttlSeconds":3600,"maxConcurrent":9}`)},true)==nil{t.Fatal("invalid concurrency permitted")}
}
func TestPrivateCredentialStorage(t *testing.T){
    dir:=t.TempDir();if err:=os.Chmod(dir,0700);err!=nil{t.Fatal(err)};path:=filepath.Join(dir,"grant.json");want:=Credential{GrantID:"g",Credential:"secret",InstanceID:"i"}
    if err:=WriteCredential(path,want);err!=nil{t.Fatal(err)};got,err:=ReadCredential(path);if err!=nil||got!=want{t.Fatal(got,err)}
    if err:=WriteCredential(path,want);err==nil{t.Fatal("existing file overwritten")}
    link:=filepath.Join(dir,"link");if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)};if _,err:=ReadCredential(link);err==nil{t.Fatal("symlink read")}
    if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)};if _,err:=ReadCredential(path);err==nil{t.Fatal("public credential read")}
}

func TestRegisteredProfileIdentifiers(t *testing.T){
    for _,profile:=range []string{"pi-report","build","test-suite"}{
        params,_:=json.Marshal(map[string]any{"actorId":"a","profile":profile,"ttlSeconds":3600,"maxConcurrent":1})
        if err:=Validate(Request{Method:"grant.create",Params:params},true);err!=nil{t.Fatal(profile,err)}
        params,_=json.Marshal(map[string]string{"profile":profile,"idempotencyKey":"k","text":"run report"})
        if err:=Validate(Request{GrantID:"g",Credential:"c",Method:"tasks.submit",Params:params},false);err!=nil{t.Fatal(profile,err)}
    }
    for _,profile:=range []string{"/bin/sh","../test","Build","a b","x;id"}{
        params,_:=json.Marshal(map[string]string{"profile":profile,"idempotencyKey":"k","text":"x"})
        if Validate(Request{GrantID:"g",Credential:"c",Method:"tasks.submit",Params:params},false)==nil{t.Fatal(profile)}
    }
}

func TestA2ADelegatedAllowlist(t *testing.T){
    for _,method:=range []string{"SendMessage","SendStreamingMessage","GetTask","ListTasks","CancelTask","SubscribeToTask","CreateTaskPushNotificationConfig","GetTaskPushNotificationConfig","ListTaskPushNotificationConfigs","DeleteTaskPushNotificationConfig","GetExtendedAgentCard","agent.card"}{
        q:=Request{GrantID:"g",Credential:"c",Method:method,Params:json.RawMessage(`{}`)}
        if err:=Validate(q,false);err!=nil{t.Fatal(method,err)}
        q.GrantID="";q.Credential="";if Validate(q,true)==nil{t.Fatal("owner bypass permitted",method)}
    }
    for _,params:=range []string{`{"id":"t","after":-1}`,`{"id":"","after":0}`,`{"id":"t","after":0,"command":"sh"}`}{if Validate(Request{GrantID:"g",Credential:"c",Method:"a2x.events",Params:json.RawMessage(params)},false)==nil{t.Fatal(params)}}
}
