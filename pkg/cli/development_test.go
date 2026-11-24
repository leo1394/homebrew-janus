package cli

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/store"
)
func TestDevelopmentDataIsolation(t *testing.T){
    first:=filepath.Join(t.TempDir(),"one");second:=filepath.Join(t.TempDir(),"two")
    a,scopeA,err:=developmentData("0.1.6-dev",first,"");if err!=nil{t.Fatal(err)}
    b,scopeB,err:=developmentData("0.1.6-dev",second,"");if err!=nil{t.Fatal(err)}
    if a.Root==b.Root||scopeA==scopeB||len(scopeA)!=64{t.Fatal("namespace collision")}
    alias:=filepath.Join(t.TempDir(),"alias");if err:=os.Symlink(filepath.Dir(first),alias);err!=nil{t.Fatal(err)}
    _,same,err:=developmentData("0.1.6-dev",filepath.Join(alias,"one"),"");if err!=nil||same!=scopeA{t.Fatal("alias has different namespace",err)}
    production,err:=store.Default();if err!=nil{t.Fatal(err)}
    for _,tc:=range []struct{version,path,socket string}{{"0.1.5",first,""},{"0.1.6-dev",production.Root,""},{"0.1.6-dev",filepath.Join(production.Root,"test"),""},{"0.1.6-dev","relative",""},{"0.1.6-dev","","/tmp/master.sock"}}{
        if _,_,err:=developmentData(tc.version,tc.path,tc.socket);err==nil{t.Fatal("unsafe development configuration accepted",tc)}
    }
    if _,err:=os.Stat(first);!os.IsNotExist(err){t.Fatal("configuration lookup created data")}
}
