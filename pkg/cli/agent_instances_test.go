package cli

import (
    "bytes"
    "errors"
    "path/filepath"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
func TestInstanceSelectionCardinality(t *testing.T){
    for _,name:=range []string{"default","research"}{
        rows:=[]janus.AgentInstance{{Name:name,Type:"pi",Status:"running"}}
        selected,err:=chooseAgentInstance(rows,func([]selector.Candidate)(selector.Candidate,bool,error){t.Fatal("single instance opened selector");return selector.Candidate{},false,nil})
        if err!=nil||selected.Name!=name{t.Fatal(selected,err)}
    }
    rows:=[]janus.AgentInstance{{Name:"research",Type:"codex",Status:"stopped"},{Name:"default",Type:"pi",Status:"running"}}
    selected,err:=chooseAgentInstance(rows,func(c []selector.Candidate)(selector.Candidate,bool,error){
        if len(c)!=2||c[0].Name!="research"||c[1].Name!="default"||!strings.Contains(c[0].Detail,"stopped"){t.Fatal(c)}
        return c[0],true,nil
    });if err!=nil||selected.Name!="research"{t.Fatal(selected,err)}
    if _,err:=chooseAgentInstance(nil,func([]selector.Candidate)(selector.Candidate,bool,error){t.Fatal("empty selector invoked");return selector.Candidate{},false,nil});err==nil{t.Fatal("empty list accepted")}
    if _,err:=chooseAgentInstance(rows,func([]selector.Candidate)(selector.Candidate,bool,error){return selector.Candidate{},false,nil});err==nil{t.Fatal("cancellation accepted")}
}
func TestSpawnInstanceIdentityAndFixedBinding(t *testing.T){
    for _,tc:=range []struct{name,explicit,discoveredID,actualID,actualName string;empty,unavailable,missing bool;wantError bool}{
        {name:"unique named",actualName:"research"},
        {name:"explicit bypasses unavailable discovery",explicit:"research",actualName:"research",unavailable:true},
        {name:"explicit missing does not fallback",explicit:"missing",missing:true,wantError:true},
        {name:"no accessible instances",empty:true,wantError:true},
        {name:"discovery unavailable",unavailable:true,wantError:true},
        {name:"discovered identity changed",discoveredID:"before",actualID:"after",wantError:true},
        {name:"wrong instance name",actualName:"other",wantError:true},
    }{t.Run(tc.name,func(t *testing.T){
        s:=store.Store{Root:filepath.Join(t.TempDir(),"base")}
        r:=&agentFake{mfa:true,infoType:"codex",taskOnly:true,infoInstance:tc.actualID,infoName:tc.actualName,instances:[]janus.AgentInstance{{Name:"research",Type:"codex",Status:"running",InstanceID:tc.discoveredID}}}
        if tc.empty{r.instances=[]janus.AgentInstance{}};if tc.unavailable{r.discoveryError=errors.New("discovery unavailable")};if tc.missing{r.routeError=errors.New("subsystem request failed")}
        cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};args:=[]string{"spawn","agent","alice@example.com"};if tc.explicit!=""{args=append(args,"--instance",tc.explicit)}
        cmd.SetArgs(args);cmd.SetOut(&bytes.Buffer{});cmd.SetErr(&bytes.Buffer{});err=cmd.Execute()
        if (err!=nil)!=tc.wantError{t.Fatal(err)}
        if tc.explicit!=""&&r.discoveries!=0{t.Fatal("explicit route performed discovery")}
        rows,listErr:=s.List();if listErr!=nil{t.Fatal(listErr)}
        if tc.wantError{if len(rows)!=0{t.Fatal("failed instance registered",rows)};if tc.missing&&(len(r.seenProfiles)!=1||r.seenProfiles[0].InstanceName!="missing"||r.prompts!=0){t.Fatal("missing instance fell back")};return}
        if len(rows)!=1||rows[0].InstanceName!="research"||rows[0].DeviceType()!="codex"||r.prompts!=1{t.Fatal(rows,r.prompts)}
        r.discoveryError=errors.New("connect must not discover")
        next,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};next.SetArgs([]string{"connect",rows[0].ID});next.SetOut(&bytes.Buffer{});next.SetErr(&bytes.Buffer{})
        before:=r.discoveries;if err:=next.Execute();err!=nil||r.discoveries!=before{t.Fatal(err,r.discoveries)}
        if r.seenProfiles[len(r.seenProfiles)-1].InstanceName!="research"{t.Fatal("connect lost named binding")}
    })}
}
