package ssh

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "strings"
    "time"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)

const agenuxDiscoveryLauncher = `/bin/sh -c 'if agenux_path=$(command -v agenux); then exec "$agenux_path" instances --json --accessible; else exec "$HOME/.local/bin/agenux" instances --json --accessible; fi'`

func(r *Runtime)DiscoverAgentInstances(ctx context.Context,p janus.Profile)([]janus.AgentInstance,error){
    // SSH authorization may require native UI; start the response deadline only
    // after the master is authenticated. Discovery never opens an instance MFA.
    args,err:=r.agenuxRegistrationArguments(ctx,p,true);if err!=nil{return nil,err}
    args[len(args)-1]="agenux-discover";if p.AgentTransport=="stdio"{args[len(args)-1]=agenuxDiscoveryLauncher}
    bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
    stream,err:=r.startAgenux(bounded,args);if err!=nil{return nil,err};defer stream.Close()
    data,err:=io.ReadAll(io.LimitReader(stream,(64<<10)+1));if err!=nil{return nil,fmt.Errorf("instance discovery failed: %w",err)}
    if bounded.Err()!=nil{return nil,bounded.Err()}
    if process,ok:=stream.(*subsystemStream);ok{if err:=process.finish();err!=nil{return nil,fmt.Errorf("instance discovery failed; deploy Agenux discovery support or specify --instance NAME: %w",err)}}
    instances,err:=ParseAgentInstances(data);if err!=nil{return nil,fmt.Errorf("instance discovery unavailable or invalid; deploy Agenux discovery support or specify --instance NAME: %w",err)}
    return instances,nil
}
func ParseAgentInstances(data []byte)([]janus.AgentInstance,error){
    if len(data)>64<<10{return nil,errors.New("instance discovery exceeds 64 KiB")}
    var response struct{Version int `json:"version"`;Instances []janus.AgentInstance `json:"instances"`}
    if err:=json.Unmarshal(data,&response);err!=nil{return nil,errors.New("invalid discovery JSON")}
    if response.Version!=1||response.Instances==nil||len(response.Instances)>128{return nil,errors.New("unsupported or incomplete instance discovery")}
    seen:=map[string]bool{}
    for _,instance:=range response.Instances{
        if !janus.ValidAgentType(instance.Name)||!janus.ValidAgentType(instance.Type)||(instance.Status!="running"&&instance.Status!="stopped")||seen[instance.Name]{return nil,errors.New("invalid or duplicate discovered instance")}
        if len(instance.InstanceID)>1024||strings.TrimSpace(instance.InstanceID)!=instance.InstanceID{return nil,errors.New("invalid discovered instance identity")}
        for _,r:=range instance.InstanceID{if r<32||r==127||(r>=128&&r<=159){return nil,errors.New("invalid discovered instance identity")}}
        seen[instance.Name]=true
    }
    return response.Instances,nil
}
