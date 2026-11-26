package database

import (
    "context"
    "database/sql"
    "errors"
    "strings"
    "testing"
)
func TestProfileSeparateSessionAndAuthorization(t *testing.T){
    c:=&testConnector{};db:=sql.OpenDB(c);db.SetMaxIdleConns(0);s:=NewSessions(db,"test");defer s.Shutdown()
    user,err:=s.Open(context.Background());if err!=nil{t.Fatal(err)};s.Query(context.Background(),user.ID,"set")
    section,err:=s.CollectProfile(context.Background(),"","");if err!=nil||section.Status!="ok"{t.Fatal(section,err)}
    if len(s.List())!=1||c.opened.Load()!=2{t.Fatal("metadata session leaked or reused user connection")}
    result,err:=s.Query(context.Background(),user.ID,"read");if err!=nil||*result.Rows[0][0]!="changed"{t.Fatal("metadata disturbed user session")}
    s.authorize=func()error{return errors.New("revoked")};before:=c.calls.Load();if _,err:=s.CollectProfile(context.Background(),"","");err==nil||c.calls.Load()!=before{t.Fatal("revoked metadata query admitted")}
}
func TestMetadataSelectionCannotInjectSQL(t *testing.T){
    value:="x'; DROP TABLE accounts; --";literal:=metadataLiteral(value)
    if strings.Contains(literal,value)||strings.Contains(literal,"DROP"){t.Fatal("unsafe SQL literal")}
    c:=&testConnector{};db:=sql.OpenDB(c);s:=NewSessions(db,"test");defer s.Shutdown()
    if _,err:=s.CollectProfile(context.Background(),"","table");err==nil||c.calls.Load()!=0{t.Fatal("invalid selector queried")}
    s.Disconnect();if _,err:=s.CollectProfile(context.Background(),"","");err==nil{t.Fatal("disconnected metadata accepted")}
}
