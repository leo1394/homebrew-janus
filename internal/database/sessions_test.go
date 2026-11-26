package database

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    "sync/atomic"
    "testing"
    "time"
)

type testConnector struct{opened atomic.Int32;closed atomic.Int32;calls atomic.Int32;entered chan struct{}}
func(c *testConnector)Connect(context.Context)(driver.Conn,error){return &testConnection{owner:c,id:c.opened.Add(1)},nil}
func(c *testConnector)Driver()driver.Driver{return testDriver{}}
type testDriver struct{}
func(testDriver)Open(string)(driver.Conn,error){return nil,errors.New("unexpected driver open")}
type testConnection struct{owner *testConnector;id int32;value string}
func(c *testConnection)Prepare(string)(driver.Stmt,error){return nil,errors.New("unexpected prepare")}
func(c *testConnection)Close()error{c.owner.closed.Add(1);return nil}
func(c *testConnection)Begin()(driver.Tx,error){return nil,errors.New("unexpected begin")}
func(c *testConnection)QueryContext(ctx context.Context,q string,_ []driver.NamedValue)(driver.Rows,error){
    c.owner.calls.Add(1)
    switch q{
    case "wait":close(c.owner.entered);<-ctx.Done();return nil,ctx.Err()
    case "write-unknown":return nil,errors.New("synthetic secret diagnostic must not escape")
    case "set":c.value="changed"
    }
    return &testRows{value:c.value},nil
}
type testRows struct{value string;done bool}
func(r *testRows)Columns()[]string{return []string{"value"}}
func(r *testRows)Close()error{return nil}
func(r *testRows)Next(v []driver.Value)error{if r.done{return io.EOF};r.done=true;v[0]=r.value;return nil}
func TestDedicatedSessionAndDisconnect(t *testing.T){
    c:=&testConnector{};db:=sql.OpenDB(c);db.SetMaxIdleConns(0);s:=NewSessions(db,"database:test");defer s.Shutdown()
    a,err:=s.Open(context.Background());if err!=nil{t.Fatal(err)};b,err:=s.Open(context.Background());if err!=nil{t.Fatal(err)}
    if c.opened.Load()!=2{t.Fatal("sessions shared a connection")}
    if _,err:=s.Query(context.Background(),a.ID,"set");err!=nil{t.Fatal(err)}
    r,err:=s.Query(context.Background(),b.ID,"read");if err!=nil||*r.Rows[0][0]!=""{t.Fatal("session state leaked",r,err)}
    s.Disconnect();if _,err:=s.Open(context.Background());err==nil{t.Fatal("disconnected owner accepted a new session")}
    r,err=s.Query(context.Background(),a.ID,"read");if err!=nil||*r.Rows[0][0]!="changed"{t.Fatal("disconnect broke active session",err)}
    if err:=s.Close(a.ID);err!=nil{t.Fatal(err)};if c.closed.Load()!=1{t.Fatal("connection not released")}
}
func TestCloseCancelsAndFences(t *testing.T){
    c:=&testConnector{entered:make(chan struct{})};db:=sql.OpenDB(c);db.SetMaxIdleConns(0);s:=NewSessions(db,"database:test");defer s.Shutdown()
    a,err:=s.Open(context.Background());if err!=nil{t.Fatal(err)}
    done:=make(chan error,1);go func(){_,err:=s.Query(context.Background(),a.ID,"wait");done<-err}()
    <-c.entered
    if err:=s.Close(a.ID);err!=nil{t.Fatal(err)}
    select{case err:=<-done:if err==nil{t.Fatal("canceled request succeeded")};case <-time.After(time.Second):t.Fatal("close did not cancel")}
    if _,err:=s.Query(context.Background(),a.ID,"read");err==nil{t.Fatal("closed session executed")}
}
func TestNoRetryAndSafeError(t *testing.T){
    c:=&testConnector{};db:=sql.OpenDB(c);s:=NewSessions(db,"database:test");defer s.Shutdown();a,err:=s.Open(context.Background());if err!=nil{t.Fatal(err)}
    _,err=s.Query(context.Background(),a.ID,"write-unknown")
    if err==nil||err.Error()!="SQL failed or connection interrupted; execution outcome may be unknown; statement was not retried"||c.calls.Load()!=1{t.Fatal("unsafe error or replay",err,c.calls.Load())}
}
