package ssh

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/deviceprofile"
    "github.com/leo1394/homebrew-janus/internal/store"
)
func TestProfileUsesOnlyMasterAndBoundedPublicOutput(t *testing.T){
    root:=t.TempDir();s:=store.Store{Root:filepath.Join(root,"base")};p,_,err:=s.Add("tester","example.com",22);if err!=nil{t.Fatal(err)}
    binary:=filepath.Join(root,"ssh");script:=`#!/bin/sh
case "$*" in *ControlMaster=no*BatchMode=yes*ProxyCommand=/usr/bin/false*) ;; *) exit 9;; esac
cat >/dev/null
printf 'system\tLinux\narchitecture\tx86_64\ncpu\tFixture\nmemory_snapshot\t1024\ngpu\tFixture\nstorage_kib\t1024\npassword\tDO_NOT_STORE\n'
printf 'DO_NOT_LOG' >&2
`
    if err:=os.WriteFile(binary,[]byte(script),0700);err!=nil{t.Fatal(err)}
    r:=Runtime{Store:s,Binary:binary};if err:=r.CollectProfile(context.Background(),p);err!=nil{t.Fatal(err)}
    snapshot,err:=deviceprofile.Read(s,p);if err!=nil{t.Fatal(err)}
    data:=string(snapshot.Sections["overview"].Data);if !strings.Contains(data,"Linux")||strings.Contains(data,"DO_NOT"){t.Fatal("metadata allowlist failed")}
    os.WriteFile(binary,[]byte("#!/bin/sh\nexit 1\n"),0700)
    if err:=r.CollectProfile(context.Background(),p);err==nil{t.Fatal("failed probe accepted")}
    snapshot,err=deviceprofile.Read(s,p);if err!=nil||snapshot.Sections["overview"].Status!="failed"||!strings.Contains(string(snapshot.Sections["overview"].Data),"Linux"){t.Fatal("failure lost prior metadata",err)}
}
