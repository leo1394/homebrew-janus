package store

import (
    "crypto/rand"
    "errors"
    "fmt"
    "strings"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)
// PrepareAgent allocates an in-memory transport identity only. No registration
// is written until A2X authentication and agent.info have succeeded.
func(s Store)PrepareAgent(user,host string,port int,transport,expected string)(janus.Profile,bool,error){
    if expected!=""&&!janus.ValidAgentType(expected){return janus.Profile{},false,errors.New("invalid agent type")}
    explicitTransport:=transport!=""
    if transport==""{transport="stdio"};kind:=expected;if kind==""{kind="pi"}
    p:=janus.Profile{Host:strings.ToLower(host),User:user,Port:port,Name:user+"@"+strings.ToLower(host),PersistSeconds:7200,Platform:"a2x",Type:kind,AgentTransport:transport}
    if err:=Validate(p);err!=nil{return p,false,err}
    rows,err:=s.List();if err!=nil{return p,false,err};matches:=[]janus.Profile{}
    for _,row:=range rows{if row.IsAgent()&&row.InstanceName==""&&Target(row)==Target(p)&&(expected==""||row.DeviceType()==expected){matches=append(matches,row)}}
    for _,row:=range rows{if !row.IsMySQL()&&Target(row)==Target(p){p.CredentialSlot=row.CredentialSlot;break}}
    if len(matches)>1{return p,false,errors.New("multiple agent devices share this target; specify --type or connect by ID")}
    if len(matches)==1{savedTransport:=matches[0].AgentTransport;if savedTransport==""{savedTransport="subsystem"};if explicitTransport&&savedTransport!=transport{return p,false,errors.New("saved agent transport differs; use connect with the saved device")};return matches[0],false,nil}
    var id [16]byte;if _,err:=rand.Read(id[:]);err!=nil{return p,false,err};id[6]=(id[6]&15)|64;id[8]=(id[8]&63)|128
    p.ID=fmt.Sprintf("agent:%x-%x-%x-%x-%x",id[:4],id[4:6],id[6:8],id[8:10],id[10:])
    return p,true,nil
}
func(s Store)RegisterAuthenticatedAgent(p janus.Profile,pending bool,instance,kind,expected string)(janus.Profile,error){
    if instance==""||!janus.ValidAgentType(kind){return p,errors.New("agent.info must provide valid instanceId and type")}
    if expected!=""&&kind!=expected{return p,errors.New("authenticated agent type does not match --type")}
    if !pending&&p.InstanceID!=""&&(p.InstanceID!=instance||p.DeviceType()!=kind){return p,errors.New("saved agent instance or type does not match authenticated identity")}
    if !pending&&p.DeviceType()!=kind&&!(p.InstanceID==""&&(p.Platform=="ai"||p.Platform=="Agenux")){return p,errors.New("saved agent type does not match authenticated identity")}
    unlock,err:=s.Lock("registry");if err!=nil{return p,err};defer unlock()
    rows,err:=s.List();if err!=nil{return p,err}
    index:=-1
    for i,row:=range rows{if row.ID==p.ID{index=i;if pending{return p,errors.New("pending device identity already exists")};if Target(row)!=Target(p)||row.InstanceID!=p.InstanceID||row.DeviceType()!=p.DeviceType()||row.AgentTransport!=p.AgentTransport||row.InstanceName!=p.InstanceName||row.CredentialSlot!=p.CredentialSlot{return p,errors.New("device changed during authentication")};p=row}}
    if !pending&&index<0{return p,errors.New("device was removed during authentication")}
    p.Platform="a2x";p.Type=kind;p.InstanceID=instance
    names:=map[string]bool{}
    for _,row:=range rows{if row.ID!=p.ID{if deviceKey(row)==deviceKey(p){return p,errors.New("authenticated device already registered; connect using its ID")};names[strings.ToLower(row.Name)]=true}}
    if pending{if len(rows)>=2000{return p,errors.New("device limit reached")};base:=p.Name;for n:=2;names[strings.ToLower(p.Name)];n++{p.Name=fmt.Sprintf("%s (%d)",base,n)}}
    if err:=Validate(p);err!=nil{return p,err}
    if pending{rows=append(rows,p)}else{rows[index]=p}
    return p,s.Save(rows)
}

// RegisterNamedAgent records a distinct authenticated deployment, never rebinding
// the source device. Existing named registrations must retain their pinned identity.
func(s Store)RegisterNamedAgent(source janus.Profile,name,instance,kind string)(janus.Profile,error){
    p:=source;p.InstanceName=name;p.InstanceID=instance;p.Type=kind;p.Platform="a2x";p.LastSession="";p.Name=source.User+"@"+source.Host+" / "+name
    if name==""||instance==""||!janus.ValidAgentType(kind){return p,errors.New("named registration requires instance name, ID and type")}
    if err:=Validate(p);err!=nil{return p,err}
    unlock,err:=s.Lock("registry");if err!=nil{return p,err};defer unlock()
    rows,err:=s.List();if err!=nil{return p,err};found:=false;var existing *janus.Profile;names:=map[string]bool{}
    for _,row:=range rows{
        if row.ID==source.ID{if row!=source{return p,errors.New("source device changed during authentication")};found=true}
        names[strings.ToLower(row.Name)]=true
        if row.IsAgent()&&Target(row)==Target(p)&&row.InstanceName==name{
            if row.InstanceID!=instance||row.DeviceType()!=kind||row.AgentTransport!=p.AgentTransport{return p,errors.New("named device identity changed; owner review required")};copy:=row;existing=&copy
        }
    }
    if !found{return p,errors.New("source device was removed during authentication")}
    if existing!=nil{return *existing,nil}
    if len(rows)>=2000{return p,errors.New("device limit reached")}
    var id [16]byte;if _,err:=rand.Read(id[:]);err!=nil{return p,err};id[6]=(id[6]&15)|64;id[8]=(id[8]&63)|128
    p.ID=fmt.Sprintf("agent:%x-%x-%x-%x-%x",id[:4],id[4:6],id[6:8],id[8:10],id[10:])
    base:=p.Name;for n:=2;names[strings.ToLower(p.Name)];n++{p.Name=fmt.Sprintf("%s (%d)",base,n)}
    return p,s.Save(append(rows,p))
}
