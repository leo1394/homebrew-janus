package deviceprofile

import (
    "os"
    "encoding/json"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/store"
)
func TestCacheIdentityFailureAndRename(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.Add("tester","example.com",22);if err!=nil{t.Fatal(err)}
    first:=Observation("fixture","ok",map[string]string{"cpu":"test"},time.Hour)
    if err:=Merge(s,p,map[string]Section{"overview":first});err!=nil{t.Fatal(err)}
    if !Fresh(s,p,"overview"){t.Fatal("fresh observation missed")}
    if err:=Merge(s,p,map[string]Section{"overview":Failure("fixture","unavailable")});err!=nil{t.Fatal(err)}
    got,err:=Read(s,p);if err!=nil{t.Fatal(err)};section:=got.Sections["overview"]
    var values map[string]string;json.Unmarshal(section.Data,&values)
    if values["cpu"]!="test"||!section.ObservedAt.Equal(first.ObservedAt)||section.Status!="failed"||Fresh(s,p,"overview"){t.Fatal("failure lost prior observation",section)}
    if err:=s.Rename(p.ID,"Renamed");err!=nil{t.Fatal(err)};p,_=s.Get(p.ID);got,err=Read(s,p);if err!=nil||got.Name!="Renamed"{t.Fatal("rename invalidated identity",err)}
    p.Host="other.example.com";if _,err:=Read(s,p);err==nil{t.Fatal("different target reused cache")}
    if err:=Merge(s,p,map[string]Section{"overview":first});err==nil{t.Fatal("changed target admitted")}
}
func TestCacheRejectsSymlinkAndOversize(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,_:=s.Add("tester","example.com",22)
    if err:=Merge(s,p,map[string]Section{"overview":Observation("fixture","ok",true,time.Hour)});err!=nil{t.Fatal(err)}
    path:=cachePath(s,p);original,_:=os.ReadFile(path);target:=filepath.Join(t.TempDir(),"outside");os.WriteFile(target,original,0600);os.Remove(path);os.Symlink(target,path)
    if _,err:=Read(s,p);err==nil{t.Fatal("symlink read allowed")}
    huge:=Observation("fixture","ok",strings.Repeat("x",MaxSize),time.Hour);if huge.Status!="failed"||len(huge.Data)!=0{t.Fatal("oversized data retained")}
}
func TestAgentOnlyDeclaredMetadata(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,pending,err:=s.PrepareAgent("tester","example.com",7263,"stdio","pi");if err!=nil{t.Fatal(err)}
    p,err=s.RegisterAuthenticatedAgent(p,pending,"instance-1","pi","");if err!=nil{t.Fatal(err)}
    info:=AgentInfo{InstanceID:"wrong",Type:"pi"};if Agent(s,p,info,true)==nil{t.Fatal("wrong instance accepted")}
    info.InstanceID=p.InstanceID;if err:=Agent(s,p,info,true);err!=nil{t.Fatal(err)}
    snapshot,err:=Read(s,p);if err!=nil{t.Fatal(err)};var values map[string]any;json.Unmarshal(snapshot.Sections["overview"].Data,&values);if values["modelsStatus"]!="not-reported"{t.Fatal("missing models guessed")}
}
func TestOptionalAgentDiscoveryDoesNotBreakIdentity(t *testing.T){
    var info AgentInfo
    if err:=json.Unmarshal([]byte(`{"instanceId":"instance-1","type":"pi","models":["different-model-schema"],"version":{},"capabilities":{"delegatedTasks":true}}`),&info);err!=nil{t.Fatal(err)}
    if info.InstanceID!="instance-1"||!info.Capabilities.DelegatedTasks||info.Models!=nil{t.Fatal("optional discovery broke identity")}
    if err:=json.Unmarshal([]byte(`{"instanceId":"instance-1","type":"pi","models":[{"id":"model-1","provider":"example","apiKey":"DO_NOT_STORE"}]}`),&info);err!=nil{t.Fatal(err)}
    data,_:=json.Marshal(info);if len(info.Models)!=1||strings.Contains(string(data),"DO_NOT_STORE"){t.Fatal("model allowlist failed")}
}
func TestAgentDiscoveryPreservesAdvertisedMethods(t *testing.T){
    var info AgentInfo
    if err:=json.Unmarshal([]byte(`{"instanceId":"fixture","type":"pi","methods":["composer.catalog","session.history","project.list"]}`),&info);err!=nil{t.Fatal(err)}
    if strings.Join(info.Methods,",")!="composer.catalog,session.history,project.list"{t.Fatalf("lost advertised methods: %v",info.Methods)}
}

func TestSSHProbeWhitespaceAndLegacyCache(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.Add("tester","example.com",22);if err!=nil{t.Fatal(err)}
    raw:=json.RawMessage(`{"memory_snapshot":"Pages free:     3960.;Pages active:   111917.;","storage_kib":"Filesystem       Used Available;/dev/disk1    12   34;","gpu":"  Chipset Model: Apple M1;  ","shell":"/path/with  spaces","other":{"note":"keep  spaces"}}`)
    section:=Observation("ssh.read-only-probe","ok",raw,time.Hour)
    var values map[string]any;if err:=json.Unmarshal(section.Data,&values);err!=nil{t.Fatal(err)}
    if values["memory_snapshot"]!="Pages free: 3960.;Pages active: 111917.;"||values["storage_kib"]!="Filesystem Used Available;/dev/disk1 12 34;"||values["shell"]!="/path/with  spaces"{t.Fatal(values)}
    if string(normalizeProbeData("mysql",raw))!=string(raw){t.Fatal("changed non-SSH metadata")}
    section.Data=raw // Simulate a cache produced by the older collector.
    if err:=Merge(s,p,map[string]Section{"resources":section});err!=nil{t.Fatal(err)}
    before,err:=os.ReadFile(cachePath(s,p));if err!=nil{t.Fatal(err)}
    snapshot,err:=Read(s,p);if err!=nil{t.Fatal(err)}
    if strings.Contains(string(snapshot.Sections["resources"].Data),"     "){t.Fatal("legacy padding retained")}
    after,_:=os.ReadFile(cachePath(s,p));if string(before)!=string(after){t.Fatal("read rewrote cache")}
    if !snapshot.Sections["resources"].ObservedAt.Equal(section.ObservedAt){t.Fatal("read changed observation time")}
}
