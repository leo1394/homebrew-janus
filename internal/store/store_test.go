package store

import (
    "errors"
    "os"
    "path/filepath"
    "sync"
    "testing"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
func testStore(t *testing.T)Store{t.Helper();return Store{Root:filepath.Join(t.TempDir(),"base")}}
func TestIdentityAndNames(t *testing.T){
    s:=testStore(t)
    a,new,err:=s.Add("alice","EXAMPLE.com",22);if err!=nil||!new{t.Fatal(a,new,err)}
    b,new,err:=s.Add("alice","example.com",22);if err!=nil||new||a.ID!=b.ID{t.Fatal(b,new,err)}
    c,_,err:=s.Add("alice","example.com",3233);if err!=nil||c.Name!="alice@example.com:3233"{t.Fatal(c,err)}
    if err:=s.Rename(c.ID,"ALICE@EXAMPLE.COM");err==nil{t.Fatal("duplicate rename allowed")}
    c2,err:=s.Get(c.ID);if err!=nil||c2.Name!=c.Name{t.Fatal(c2,err)}
    if err:=s.Rename(a.ID,"Build Server");err!=nil{t.Fatal(err)}
    if p,err:=s.Get(a.ID);err!=nil||p.ID!=a.ID||p.Name!="Build Server"{t.Fatal(p,err)}
}
func TestRemoveCredentialFailureAndActiveIndependence(t *testing.T){
    s:=testStore(t);p,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)}
    err=s.Remove(p.ID,func(janus.Profile)error{return errors.New("denied")});if err==nil{t.Fatal("failed deletion accepted")}
    if _,err:=s.Get(p.ID);err!=nil{t.Fatal("record removed on credential failure",err)}
    called:=false
    if err:=s.Remove(p.ID,func(got janus.Profile)error{called=true;if got.ID!=p.ID{t.Fatal(got)};return nil});err!=nil{t.Fatal(err)}
    if !called{t.Fatal("credential was not deleted")};rows,err:=s.List();if err!=nil||len(rows)!=0{t.Fatal(rows,err)}
}
func TestConcurrentAddDeduplicates(t *testing.T){
    s:=testStore(t);var wg sync.WaitGroup
    for i:=0;i<12;i++{wg.Add(1);go func(){defer wg.Done();if _,_,err:=s.Add("alice","example.com",22);err!=nil{t.Error(err)}}()};wg.Wait()
    rows,err:=s.List();if err!=nil||len(rows)!=1{t.Fatal(rows,err)}
}
func TestRejectSymlinksAndInvalidNames(t *testing.T){
    s:=testStore(t);p,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)}
    for _,name:=range []string{"", "bad\nname", "bad\x1b[31m", "bad\x7fname"}{if err:=s.Rename(p.ID,name);err==nil{t.Fatal("invalid name accepted",name)}}
    path:=filepath.Join(s.Root,"devices.json");if err:=os.Remove(path);err!=nil{t.Fatal(err)}
    if err:=os.Symlink(filepath.Join(t.TempDir(),"other"),path);err!=nil{t.Fatal(err)}
    if _,err:=s.List();err==nil{t.Fatal("registry symlink accepted")}
}
func TestCorruptRegistryFailsClosed(t *testing.T){
    s:=testStore(t);if err:=SecureDir(s.Root);err!=nil{t.Fatal(err)}
    path:=filepath.Join(s.Root,"devices.json")
    for _,data:=range []string{`{"schemaVersion":9,"profiles":[]}`,`{"schemaVersion":1,"profiles":[]} {}`,`{"schemaVersion":1,"profiles":[],"password":"secret"}`} {
        if err:=os.WriteFile(path,[]byte(data),0600);err!=nil{t.Fatal(err)}
        if _,_,err:=s.Add("alice","example.com",22);err==nil{t.Fatal("corrupt registry overwritten")}
    }
}
func TestRejectParentSymlinkBeforeCreation(t *testing.T){
    root:=t.TempDir();target:=filepath.Join(root,"target");os.Mkdir(target,0700)
    parent:=filepath.Join(root,"redirect");if err:=os.Symlink(target,parent);err!=nil{t.Fatal(err)}
    s:=Store{Root:filepath.Join(parent,"base")}
    if _,_,err:=s.Add("alice","example.com",22);err==nil{t.Fatal("redirected parent accepted")}
    if _,err:=os.Stat(filepath.Join(target,"base"));!os.IsNotExist(err){t.Fatal("created files before parent validation",err)}
}
func TestAgentAndTerminalIdentityAndCredentialLifetime(t *testing.T){
    s:=testStore(t);terminal,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)}
    agent,created,err:=s.AddAgent("alice","example.com",22,"subsystem");if err!=nil||!created||!agent.IsAgent()||agent.ID==terminal.ID{t.Fatal(agent,err)}
    if err:=s.SetSession(agent.ID,"session-1");err!=nil{t.Fatal(err)}
    again,created,err:=s.AddAgent("alice","example.com",22,"stdio");if err!=nil||created||again.ID!=agent.ID||again.LastSession!="session-1"||again.AgentTransport!="stdio"{t.Fatal(again,err)}
    deleted:=0
    cleanup:=func(janus.Profile)error{deleted++;return nil}
    if err:=s.Remove(terminal.ID,cleanup);err!=nil||deleted!=0{t.Fatal("shared credential removed",err,deleted)}
    if err:=s.Remove(agent.ID,cleanup);err!=nil||deleted!=1{t.Fatal("last credential not removed",err,deleted)}
}

func TestAgentIdentityMigration(t *testing.T){
    s:=Store{Root:filepath.Join(t.TempDir(),"base")}
    p,_,err:=s.AddAgent("alice","example.com",22,"stdio");if err!=nil{t.Fatal(err)}
    p.Platform="ai";p.Type="agent";p.LastSession="existing-session"
    if err:=s.Save([]janus.Profile{p});err!=nil{t.Fatal(err)}
    rows,err:=s.List();if err!=nil||rows[0].DevicePlatform()!="a2x"||rows[0].DeviceType()!="pi"{t.Fatal(rows,err)}
    duplicate,created,err:=s.AddAgent("alice","EXAMPLE.com",22,"stdio");if err!=nil||created||duplicate.ID!=p.ID{t.Fatal(duplicate,created,err)}
    if err:=s.BindAgentInstance(p.ID,"instance-1");err!=nil{t.Fatal(err)}
    got,err:=s.Get(p.ID);if err!=nil||got.InstanceID!="instance-1"||got.Platform!="a2x"||got.Type!="pi"||got.LastSession!=p.LastSession||Target(got)!=Target(p){t.Fatal(got,err)}
    if err:=s.BindAgentInstance(p.ID,"other-instance");err==nil{t.Fatal("silently accepted identity change")}
    got,_=s.Get(p.ID);if got.InstanceID!="instance-1"{t.Fatal(got)}
}

func TestCodexIdentityBinding(t *testing.T){
    s:=testStore(t);p,_,err:=s.AddAgent("a","example.com",22,"stdio");if err!=nil{t.Fatal(err)}
    if err:=s.BindAgentIdentity(p.ID,"instance-codex","codex");err!=nil{t.Fatal(err)}
    got,err:=s.Get(p.ID);if err!=nil||got.Type!="codex"||got.DeviceType()!="codex"||!got.IsAgent(){t.Fatal(got,err)}
    same,created,err:=s.AddAgentType("a","example.com",22,"stdio","codex");if err!=nil||created||same.ID!=p.ID{t.Fatal(same,created,err)}
    if err:=s.BindAgentIdentity(p.ID,"different","pi");err==nil{t.Fatal("instance pin lost")}
    if err:=s.BindAgentIdentity(p.ID,"instance-codex","../shell");err==nil{t.Fatal("unknown type accepted")}
}

func TestA2XLegacyAndNamedTypes(t *testing.T){
    for _,kind:=range []string{"pi","codex","opencode","claude-code","research-acp"}{t.Run(kind,func(t *testing.T){
        s:=testStore(t);p,_,err:=s.AddAgentType("alice","example.com",22,"stdio",kind);if err!=nil{t.Fatal(err)}
        if p.Platform!="a2x"{t.Fatal(p)}
        p.Platform="Agenux";p.LastSession="preserved";p.InstanceID="instance-preserved";if err:=s.Save([]janus.Profile{p});err!=nil{t.Fatal(err)}
        before,err:=s.Get(p.ID);if err!=nil||before.DevicePlatform()!="a2x"||before.DeviceType()!=kind{t.Fatal(before,err)}
        migrated,created,err:=s.AddAgentType("alice","EXAMPLE.com",22,"stdio",kind)
        if err!=nil||created||migrated.ID!=p.ID||migrated.Platform!="a2x"||migrated.LastSession!=p.LastSession||migrated.InstanceID!=p.InstanceID||Target(migrated)!=Target(p){t.Fatal(migrated,created,err)}
        if err:=s.BindAgentIdentity(p.ID,"different",kind);err==nil{t.Fatal("instance pin changed")}
    })}
    for _,kind:=range []string{"","../agent","agent;sh","Agent", "a b"}{s:=testStore(t);if _,_,err:=s.AddAgentType("a","example.com",22,"stdio",kind);err==nil{t.Fatal(kind)}}
}

func TestNamedRegistrationIsolation(t *testing.T){
    s:=testStore(t);source,_,err:=s.AddAgent("owner","example.com",7263,"stdio");if err!=nil{t.Fatal(err)}
    source.InstanceID="default";source.LastSession="kept";if err:=s.Save([]janus.Profile{source});err!=nil{t.Fatal(err)}
    named,err:=s.RegisterNamedAgent(source,"test","named-id","opencode");if err!=nil{t.Fatal(err)}
    original,_:=s.Get(source.ID);if original!=source||named.ID==source.ID||named.LastSession!=""{t.Fatal(original,named)}
    repeated,err:=s.RegisterNamedAgent(source,"test","named-id","opencode");if err!=nil||repeated!=named{t.Fatal(repeated,err)}
    for _,value:=range []struct{name,id,kind string}{{"test","changed","opencode"},{"test","named-id","pi"},{"x;id","new","pi"},{"other","new",""}}{if _,err:=s.RegisterNamedAgent(source,value.name,value.id,value.kind);err==nil{t.Fatal("invalid identity accepted",value)}}
    rows,err:=s.List();if err!=nil||len(rows)!=2{t.Fatal(rows,err)}
    prepared,pending,err:=s.PrepareAgent("owner","example.com",7263,"stdio","");if err!=nil||pending||prepared.ID!=source.ID{t.Fatal(prepared,pending,err)}
}

func TestPrepareAgentDefaultSubsystemPreservesSavedTransport(t *testing.T){
    s:=Store{Root:filepath.Join(t.TempDir(),"base")}
    fresh,pending,err:=s.PrepareAgent("alice","new.example.com",7263,"","")
    if err!=nil||!pending||fresh.AgentTransport!="subsystem"{t.Fatal(fresh,pending,err)}
    explicit,_,err:=s.PrepareAgent("alice","new.example.com",7263,"stdio","")
    if err!=nil||explicit.AgentTransport!="stdio"{t.Fatal(explicit,err)}
    saved,_,err:=s.AddAgent("alice","saved.example.com",7263,"stdio");if err!=nil{t.Fatal(err)}
    got,pending,err:=s.PrepareAgent("alice","saved.example.com",7263,"","")
    if err!=nil||pending||got.ID!=saved.ID||got.AgentTransport!="stdio"{t.Fatal(got,pending,err)}
}

func TestListPlatformOrderStableAndReadOnly(t *testing.T){
    s:=Store{Root:filepath.Join(t.TempDir(),"base")}
    agent,_,err:=s.AddAgent("alice","agent.example.com",22,"stdio");if err!=nil{t.Fatal(err)}
    first,_,err:=s.Add("alice","z.example.com",22);if err!=nil{t.Fatal(err)}
    second,_,err:=s.Add("alice","a.example.com",22);if err!=nil{t.Fatal(err)}
    if err:=s.Save([]janus.Profile{agent,first,second});err!=nil{t.Fatal(err)}
    path:=filepath.Join(s.Root,"devices.json");before,_:=os.ReadFile(path)
    rows,err:=s.List();if err!=nil{t.Fatal(err)}
    if rows[0].ID!=first.ID||rows[1].ID!=second.ID||rows[2].ID!=agent.ID{t.Fatal("platform or stable order incorrect",rows)}
    after,_:=os.ReadFile(path);if string(before)!=string(after){t.Fatal("listing rewrote registry")}
}
