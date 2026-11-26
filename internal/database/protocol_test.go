package database

import (
    "context"
    "database/sql"
    "net"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestIPCConnectionBoundAndCleanup(t *testing.T){
    dir,err:=os.MkdirTemp("/private/tmp","jdb-");if err!=nil{t.Fatal(err)};defer os.RemoveAll(dir)
    path:=filepath.Join(dir,"owner.sock")
    listener,err:=net.ListenUnix("unix",&net.UnixAddr{Name:path,Net:"unix"});if err!=nil{t.Fatal(err)}
    if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
    connector:=&testConnector{};db:=sql.OpenDB(connector);db.SetMaxIdleConns(0)
    sessions:=NewSessions(db,"database:test")
    ctx,cancel:=context.WithCancel(context.Background());defer cancel()
    done:=make(chan error,1);go func(){done<-Serve(ctx,listener,sessions)}()
    a,err:=Dial(ctx,path);if err!=nil{t.Fatal(err)};defer a.Close()
    b,err:=Dial(ctx,path);if err!=nil{t.Fatal(err)};defer b.Close()
    opened,err:=a.Call(ctx,Request{Method:"open"});if err!=nil{t.Fatal(err)}
    if _,err:=b.Call(ctx,Request{Method:"query",SessionID:opened.Session.ID,SQL:"set"});err==nil{t.Fatal("another connection used session ID as authorization")}
    if connector.calls.Load()!=0{t.Fatal("unauthorized SQL reached driver")}
    if _,err:=a.Call(ctx,Request{Method:"query",SessionID:opened.Session.ID,SQL:"set"});err!=nil{t.Fatal(err)}
    if _,err:=b.Call(ctx,Request{Method:"revoke",SessionID:opened.Session.ID});err!=nil{t.Fatal(err)}
    if _,err:=a.Call(ctx,Request{Method:"query",SessionID:opened.Session.ID,SQL:"read"});err==nil{t.Fatal("revoked session usable")}
    cancel();select{case err:=<-done:if err!=nil{t.Fatal(err)};case <-time.After(2*time.Second):t.Fatal("owner shutdown stalled")}
}
func TestAdmissionAndCredentialRevocation(t *testing.T){
    connector:=&testConnector{};db:=sql.OpenDB(connector);s:=NewSessions(db,"database:test");defer s.Shutdown()
    ctx:=context.Background();a,err:=s.Open(ctx);if err!=nil{t.Fatal(err)}
    s.admit=func()error{return context.Canceled}
    if _,err:=s.Open(ctx);err==nil{t.Fatal("removed target admitted new session")}
    if _,err:=s.Query(ctx,a.ID,"read");err!=nil{t.Fatal("target removal killed active session")}
    s.authorize=func()error{return context.Canceled}
    if _,err:=s.Query(ctx,a.ID,"read");err==nil{t.Fatal("credential revocation allowed query")}
}
func TestInvalidCABeforeConnection(t *testing.T){
    if err:=ValidateTarget(Target{Host:"localhost",User:"test",Port:3306,CAPEM:"not a certificate"});err==nil{t.Fatal("invalid CA accepted")}
}
