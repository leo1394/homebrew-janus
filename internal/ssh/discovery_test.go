package ssh

import (
    "encoding/json"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)
func TestAgentDiscoveryContract(t *testing.T){
    for _,tc:=range []struct{name,body string;ok bool}{
        {"single default",`{"version":1,"instances":[{"name":"default","type":"pi","status":"running"}]}`,true},
        {"single named",`{"version":1,"instances":[{"name":"research","type":"codex","status":"stopped","instanceId":"id-1"}]}`,true},
        {"empty",`{"version":1,"instances":[]}`,true},
        {"duplicate",`{"version":1,"instances":[{"name":"default","type":"pi","status":"running"},{"name":"default","type":"codex","status":"running"}]}`,false},
        {"missing list",`{"version":1}`,false},{"unsupported",`{"version":2,"instances":[]}`,false},
        {"injection",`{"version":1,"instances":[{"name":"x;id","type":"pi","status":"running"}]}`,false},
        {"unknown status",`{"version":1,"instances":[{"name":"a","type":"pi","status":"ready"}]}`,false},
        {"identity controls",`{"version":1,"instances":[{"name":"a","type":"pi","status":"running","instanceId":"id\n"}]}`,false},
        {"trailing data",`{"version":1,"instances":[]} {}`,false},
        {"old server",`subsystem request failed`,false},
    }{t.Run(tc.name,func(t *testing.T){_,err:=ParseAgentInstances([]byte(tc.body));if (err==nil)!=tc.ok{t.Fatal(err)}})}
    if _,err:=ParseAgentInstances([]byte(strings.Repeat(" ",65537)));err==nil{t.Fatal("oversized response accepted")}
    data,_:=json.Marshal(map[string]any{"version":1,"instances":make([]janus.AgentInstance,129)})
    if _,err:=ParseAgentInstances(data);err==nil{t.Fatal("too many instances accepted")}
}
