package ssh

import (
    "context"
    "errors"
    "net"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "sync"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
func fixture(t *testing.T)(*Runtime,janus.Profile){
    t.Helper();root,err:=os.MkdirTemp("/private/tmp","jrt-");if err!=nil{t.Fatal(err)};t.Cleanup(func(){os.RemoveAll(root)})
    s:=store.Store{Root:root};p,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)}
    if err:=store.SecureDir(filepath.Join(root,"ssh"));err!=nil{t.Fatal(err)}
    return &Runtime{Store:s,Helper:"/fixture/keychain"},p
}
func socket(t *testing.T,r *Runtime,p janus.Profile){t.Helper();l,err:=net.Listen("unix",r.path(p));if err!=nil{t.Fatal(err)};t.Cleanup(func(){l.Close()})}
func TestReuseDoesNotLaunchOrAuthorize(t *testing.T){
    r,p:=fixture(t);socket(t,r,p);calls:=0
    r.Run=func(_ context.Context,args,env []string,terminal bool)error{calls++;if !strings.Contains(strings.Join(args," "),"-O check")||env!=nil{t.Fatal(args,env)};return nil}
    if err:=r.Ensure(context.Background(),p);err!=nil{t.Fatal(err)};if calls!=1{t.Fatal(calls)}
}
func TestDisconnectStopsNotExits(t *testing.T){
    r,p:=fixture(t);socket(t,r,p);calls:=[]string{}
    r.Run=func(_ context.Context,args,env []string,terminal bool)error{calls=append(calls,strings.Join(args," "));return nil}
    if err:=r.Disconnect(context.Background(),p);err!=nil{t.Fatal(err)}
    if len(calls)!=2||!strings.Contains(calls[1],"-O stop")||strings.Contains(calls[1],"-O exit"){t.Fatal(calls)}
    if _,err:=r.Store.Get(p.ID);err!=nil{t.Fatal("disconnect removed device",err)}
}
func TestAttachmentCannotReauthenticate(t *testing.T){
    r,p:=fixture(t)
    r.Run=func(_ context.Context,args,env []string,terminal bool)error{
        text:=strings.Join(args," ");for _,want:=range []string{"BatchMode=yes","ControlMaster=no","ProxyCommand=/usr/bin/false"}{if !strings.Contains(text,want){t.Fatal(args)}}
        if env!=nil||!terminal{t.Fatal(env,terminal)};return errors.New("master unavailable")
    }
    if err:=r.Attach(context.Background(),p);err==nil{t.Fatal("attachment failure hidden")}
}
func TestMasterUsesOnlyNativePasswordPrompt(t *testing.T){
    r,p:=fixture(t);calls:=0
    r.Run=func(_ context.Context,args,env []string,terminal bool)error{
        calls++;text:=strings.Join(args," ")
        if calls==1{
            for _,want:=range []string{"-M -N -f","ControlPersist=7200","PreferredAuthentications=password","NumberOfPasswordPrompts=1"}{if !strings.Contains(text,want){t.Fatal(args)}}
            if !strings.Contains(strings.Join(env," "),"SSH_ASKPASS_REQUIRE=force"){t.Fatal(env)}
            socket(t,r,p)
        };return nil
    }
    if err:=r.Ensure(context.Background(),p);err!=nil{t.Fatal(err)};if calls!=2{t.Fatal(calls)}
}
func TestControlPathRefusesRegularFile(t *testing.T){r,p:=fixture(t);os.WriteFile(r.path(p),[]byte("not a socket"),0600);if _,err:=r.Connected(context.Background(),p);err==nil{t.Fatal("unsafe control path accepted")}}
func TestConcurrentEnsureCreatesOneMaster(t *testing.T){
    r,p:=fixture(t);created:=0
    r.Run=func(_ context.Context,args,env []string,terminal bool)error{
        if strings.Contains(strings.Join(args," "),"-M -N -f"){created++;socket(t,r,p)}
        return nil
    }
    var wg sync.WaitGroup
    for i:=0;i<8;i++{wg.Add(1);go func(){defer wg.Done();if err:=r.Ensure(context.Background(),p);err!=nil{t.Error(err)}}()}
    wg.Wait();if created!=1{t.Fatal("duplicate masters",created)}
}
