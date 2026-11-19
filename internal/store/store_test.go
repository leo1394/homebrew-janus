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
