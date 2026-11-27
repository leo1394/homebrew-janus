// Package deviceprofile stores observations, never credentials or authorization.
package deviceprofile

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "io"
    "os"
    "path/filepath"
    "strings"
    "syscall"
    "time"

    "golang.org/x/sys/unix"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

const MaxSize = 1 << 20

type Section struct {
    Source string `json:"source"`
    Status string `json:"status"`
    ObservedAt time.Time `json:"observedAt,omitempty"`
    ExpiresAt time.Time `json:"expiresAt,omitempty"`
    AttemptedAt time.Time `json:"attemptedAt"`
    Error string `json:"error,omitempty"`
    Data json.RawMessage `json:"data,omitempty"`
}
type Snapshot struct {
    SchemaVersion int `json:"schemaVersion"`
    DeviceID string `json:"deviceId"`
    Name string `json:"name"`
    Platform string `json:"platform"`
    Type string `json:"type"`
    Identity string `json:"identity"`
    Sections map[string]Section `json:"sections"`
}
func identity(s store.Store,p janus.Profile)(string,error){
    p.Name="";p.LastSession="";p.PersistSeconds=0
    var revision uint64
    if p.AccountID!=""{a,err:=s.Account(p.AccountID);if err!=nil{return "",err};revision=uint64(a.Revision)}
    data,_:=json.Marshal(struct{Device janus.Profile;Revision uint64}{p,revision});sum:=sha256.Sum256(data);return hex.EncodeToString(sum[:]),nil
}
func cachePath(s store.Store,p janus.Profile)string{sum:=sha256.Sum256([]byte(p.ID));return filepath.Join(s.Root,"profiles",hex.EncodeToString(sum[:])+".json")}
func Read(s store.Store,p janus.Profile)(Snapshot,error){
    binding,err:=identity(s,p);if err!=nil{return Snapshot{},err}
    path:=cachePath(s,p)
    dir,err:=os.Lstat(filepath.Dir(path));if err!=nil{return Snapshot{},err};if !dir.IsDir()||dir.Mode().Perm()&0077!=0||dir.Sys().(*syscall.Stat_t).Uid!=uint32(os.Getuid()){return Snapshot{},errors.New("unsafe profile directory")}
    fd,err:=syscall.Open(path,syscall.O_RDONLY|syscall.O_NOFOLLOW|unix.O_CLOEXEC,0);if err!=nil{return Snapshot{},err}
    file:=os.NewFile(uintptr(fd),path);defer file.Close();info,err:=file.Stat();if err!=nil{return Snapshot{},err}
    if !info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||info.Size()>MaxSize||info.Sys().(*syscall.Stat_t).Uid!=uint32(os.Getuid()){return Snapshot{},errors.New("unsafe profile cache")}
    var snapshot Snapshot;dec:=json.NewDecoder(io.LimitReader(file,MaxSize));if err:=dec.Decode(&snapshot);err!=nil{return snapshot,errors.New("invalid profile cache")}
    var extra any;if dec.Decode(&extra)!=io.EOF{return Snapshot{},errors.New("trailing profile content")}
    if snapshot.SchemaVersion!=1||snapshot.DeviceID!=p.ID||snapshot.Identity!=binding{return Snapshot{},errors.New("profile identity changed; refresh required")}
    for key,section:=range snapshot.Sections{section.Data=normalizeProbeData(section.Source,section.Data);snapshot.Sections[key]=section}
    snapshot.Name=p.Name;return snapshot,nil
}
func Observation(source,status string,data any,ttl time.Duration)Section{
    now:=time.Now().UTC();encoded,err:=json.Marshal(data)
    if err!=nil||len(encoded)>MaxSize/2{return Failure(source,"profile output limit exceeded")}
    return Section{Source:source,Status:status,ObservedAt:now,ExpiresAt:now.Add(ttl),AttemptedAt:now,Data:normalizeProbeData(source,encoded)}
}
// Compact command-generated alignment only; identifiers, comments and other
// providers retain their original text. Read also handles existing caches.
func normalizeProbeData(source string,data json.RawMessage)json.RawMessage{
    if source!="ssh.read-only-probe"{return data}
    var fields map[string]json.RawMessage;if json.Unmarshal(data,&fields)!=nil{return data}
    changed:=false
    for _,key:=range []string{"memory_snapshot","storage_kib","gpu"}{
        var value string;if json.Unmarshal(fields[key],&value)!=nil{continue}
        compact:=strings.Join(strings.Fields(value)," ")
        if compact!=value{fields[key],_=json.Marshal(compact);changed=true}
    }
    if !changed{return data};encoded,err:=json.Marshal(fields);if err!=nil{return data};return encoded
}
func Failure(source,message string)Section{return Section{Source:source,Status:"failed",AttemptedAt:time.Now().UTC(),Error:message}}
// Merge is atomic and retains the previous observation after a failed refresh.
func Merge(s store.Store,p janus.Profile,updates map[string]Section)error{
    unlock,err:=s.Lock("device-profiles");if err!=nil{return err};defer unlock()
    current,err:=s.Get(p.ID);if err!=nil{return err};binding,err:=identity(s,p);if err!=nil{return err};actual,err:=identity(s,current);if err!=nil||binding!=actual{return errors.New("device changed during profile collection")}
    snapshot,err:=Read(s,p);if err!=nil{snapshot=Snapshot{SchemaVersion:1,DeviceID:p.ID,Name:current.Name,Platform:p.DevicePlatform(),Type:p.DeviceType(),Identity:binding,Sections:map[string]Section{}}}
    if snapshot.Sections==nil{snapshot.Sections=map[string]Section{}}
    for key,next:=range updates{if old,ok:=snapshot.Sections[key];ok&&next.AttemptedAt.Before(old.AttemptedAt){continue};if len(key)>300{return errors.New("invalid profile section")};if next.Status=="failed"{if old,ok:=snapshot.Sections[key];ok{next.Data=old.Data;next.ObservedAt=old.ObservedAt;next.ExpiresAt=old.ExpiresAt}};snapshot.Sections[key]=next}
    data,err:=json.MarshalIndent(snapshot,"","  ");if err!=nil{return err};if len(data)>MaxSize{return errors.New("profile cache limit exceeded")}
    dir:=filepath.Dir(cachePath(s,p));if err:=store.SecureDir(dir);err!=nil{return err}
    file,err:=os.CreateTemp(dir,".profile-");if err!=nil{return err};defer os.Remove(file.Name())
    if _,err=file.Write(data);err==nil{err=file.Sync()};closeErr:=file.Close();if err!=nil{return err};if closeErr!=nil{return closeErr};return os.Rename(file.Name(),cachePath(s,p))
}
func Fresh(s store.Store,p janus.Profile,key string)bool{snapshot,err:=Read(s,p);if err!=nil{return false};section,ok:=snapshot.Sections[key];return ok&&section.Status!="failed"&&time.Now().Before(section.ExpiresAt)}
// SafeText prevents remote metadata from injecting terminal control sequences.
func SafeText(value string)string{return strings.Map(func(r rune)rune{if r<32||r==127||r>=128&&r<=159{return -1};return r},value)}

type Model struct {
    ID string `json:"id"`
    Name string `json:"name,omitempty"`
    Provider string `json:"provider,omitempty"`
    ContextWindow int `json:"contextWindow,omitempty"`
}
type AgentPrincipal struct {ID string `json:"id"`;Username string `json:"username"`}
type AgentInfo struct {
    Principal *AgentPrincipal `json:"principal,omitempty"`
    Methods []string `json:"methods,omitempty"`
    InstanceName string `json:"instanceName,omitempty"`
    InstanceID string `json:"instanceId"`
    Type string `json:"type"`
    Version string `json:"version,omitempty"`
    Capabilities struct {
        WorkspaceIsolation string `json:"workspaceIsolation,omitempty"`
        NativePTY bool `json:"nativePty"`
        ClientTools bool `json:"clientTools"`
        DelegatedTasks bool `json:"delegatedTasks"`
    } `json:"capabilities"`
    Models []Model `json:"models,omitempty"`
    CurrentModel string `json:"currentModel,omitempty"`
}
func Agent(s store.Store,p janus.Profile,info AgentInfo,terminal bool)error{
    if info.InstanceID!=p.InstanceID||info.Type!=p.DeviceType(){return errors.New("agent profile identity mismatch")}
    if len(info.Models)>256{return errors.New("agent model list exceeds limit")}
    // Principal belongs to this authenticated connection, not the device cache.
    info.Principal=nil;info.Capabilities.WorkspaceIsolation=""
    modelStatus:="not-reported";if info.Models!=nil{modelStatus="server-declared"}
    data:=struct{AgentInfo;Terminal bool `json:"terminal"`;ModelsStatus string `json:"modelsStatus"`;Authorization string `json:"authorization"`}{info,terminal,modelStatus,"descriptive-only; execution requires current authorization"}
    return Merge(s,p,map[string]Section{"overview":Observation("a2x.agent.info","ok",data,24*time.Hour)})
}

// Optional discovery fields are not part of the mandatory identity contract.
// A server with another model schema must not break normal agent connection.
func(info *AgentInfo)UnmarshalJSON(data []byte)error{
    var wire struct {
        Principal json.RawMessage `json:"principal"`
        InstanceName string `json:"instanceName"`
        InstanceID string `json:"instanceId"`
        Type string `json:"type"`
        Version json.RawMessage `json:"version"`
        Capabilities json.RawMessage `json:"capabilities"`
        Methods json.RawMessage `json:"methods"`
        Models json.RawMessage `json:"models"`
        CurrentModel json.RawMessage `json:"currentModel"`
    }
    if err:=json.Unmarshal(data,&wire);err!=nil{return err}
    if wire.InstanceName!=""&&!janus.ValidAgentType(wire.InstanceName){return errors.New("invalid agent instance name")}
    *info=AgentInfo{InstanceName:wire.InstanceName,InstanceID:wire.InstanceID,Type:wire.Type}
    _ = json.Unmarshal(wire.Version,&info.Version)
    _ = json.Unmarshal(wire.Capabilities,&info.Capabilities)
    var scope struct{WorkspaceIsolation string `json:"workspaceIsolation"`}
    var capabilities map[string]json.RawMessage
    if json.Unmarshal(wire.Capabilities,&capabilities)==nil{if raw,ok:=capabilities["workspaceIsolation"];ok{if json.Unmarshal(raw,&scope.WorkspaceIsolation)!=nil{return errors.New("invalid workspace isolation capability")}}}
    info.Capabilities.WorkspaceIsolation=scope.WorkspaceIsolation
    if len(wire.Principal)>0&&string(wire.Principal)!="null"{
        var principal AgentPrincipal
        if json.Unmarshal(wire.Principal,&principal)!=nil||!validPrincipalText(principal.ID)||!validPrincipalText(principal.Username){return errors.New("invalid agent principal")}
        info.Principal=&principal
    }
    if scope.WorkspaceIsolation=="principal-v1"&&info.Principal==nil{return errors.New("isolated workspace requires authenticated principal")}
    _ = json.Unmarshal(wire.CurrentModel,&info.CurrentModel)
    var methods []string;if json.Unmarshal(wire.Methods,&methods)==nil{info.Methods=methods}
    var models []Model;if json.Unmarshal(wire.Models,&models)==nil&&len(models)<=256{
        valid:=true;for _,model:=range models{if model.ID==""||len(model.ID)>256||len(model.Name)>512||len(model.Provider)>256{valid=false;break}};if valid{info.Models=models}
    }
    return nil
}

func validPrincipalText(value string)bool{
    if value==""||len(value)>256||strings.TrimSpace(value)!=value{return false}
    for _,r:=range value{if r<32||r==127||(r>=128&&r<=159){return false}}
    return true
}
