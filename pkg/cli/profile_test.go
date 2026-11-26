package cli

import (
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/deviceprofile"
    "github.com/leo1394/homebrew-janus/internal/store"
)
func TestProfileOfflineAndSelection(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.Add("tester","invalid.example",22);if err!=nil{t.Fatal(err)}
    if _,err:=execute(t,s,"profile",p.ID);err==nil||!strings.Contains(err.Error(),"no cached profile"){t.Fatal(err)}
    if err:=deviceprofile.Merge(s,p,map[string]deviceprofile.Section{"overview":deviceprofile.Observation("fixture","ok",map[string]string{"cpu":"test"},time.Hour)});err!=nil{t.Fatal(err)}
    out,err:=execute(t,s,"profile",p.ID,"--json");if err!=nil||!strings.Contains(out,`"cpu":"test"`){t.Fatal(out,err)}
    if _,err:=execute(t,s,"profile",p.ID,"--database","app");err==nil{t.Fatal("SSH accepted database selection")}
    if _,err:=execute(t,s,"profile",p.ID,"--table","orders");err==nil{t.Fatal("table without database accepted")}
}
func TestAllDatabaseProfilesOnlyListsVisibleNames(t *testing.T){
    snapshot:=deviceprofile.Snapshot{Sections:map[string]deviceprofile.Section{
        "overview":deviceprofile.Observation("fixture","ok",map[string]any{"databases":map[string]any{"columns":[]string{"Database"},"rows":[][]string{{"app"},{"logs"}}}},time.Hour),
        "database:617070":deviceprofile.Observation("fixture","ok",map[string]string{"table":"orders"},time.Hour),
        "database:68696464656e":deviceprofile.Observation("fixture","ok",map[string]string{"table":"private"},time.Hour),
    }}
    result,err:=allDatabaseProfiles(snapshot);if err!=nil{t.Fatal(err)}
    if len(result.Sections)!=3||result.Sections["database: logs"].Status!="not-collected"||result.Sections["database: app"].Status!="ok"{t.Fatal("visible list or missing metadata incorrect",result)}
}
func TestAllDatabaseFlagValidation(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,_:=s.Add("tester","invalid.example",22)
    for _,args:=range [][]string{{"profile",p.ID,"--show-all"},{"profile",p.ID,"--show-all","--database","app"}}{if _,err:=execute(t,s,args...);err==nil{t.Fatal("invalid all-database request accepted")}}
}
func TestMySQLProfileFreshRequiresEveryDirectory(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,_:=s.Add("tester","invalid.example",22)
    overview:=deviceprofile.Observation("fixture","ok",map[string]any{"databases":map[string]any{"rows":[][]string{{"app"},{"logs"}}}},time.Hour)
    directory:=deviceprofile.Observation("fixture","ok",map[string]any{"tables":[]string{}},time.Hour)
    if err:=deviceprofile.Merge(s,p,map[string]deviceprofile.Section{"overview":overview,"database:617070":directory});err!=nil{t.Fatal(err)}
    if mysqlProfileFresh(s,p){t.Fatal("missing directory skipped refresh")}
    if err:=deviceprofile.Merge(s,p,map[string]deviceprofile.Section{"database:6c6f6773":directory});err!=nil{t.Fatal(err)}
    if !mysqlProfileFresh(s,p){t.Fatal("complete fresh cache rejected")}
    if err:=deviceprofile.Merge(s,p,map[string]deviceprofile.Section{"database:6c6f6773":deviceprofile.Failure("fixture","failed")});err!=nil{t.Fatal(err)}
    if mysqlProfileFresh(s,p){t.Fatal("failed directory skipped refresh")}
}
