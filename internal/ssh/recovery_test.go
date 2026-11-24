package ssh

import (
    "context"
    "errors"
    "net"
    "os"
    "path/filepath"
    "strings"
    "testing"
)
func TestRecoveryVerifiesBeforeSelecting(t *testing.T){
    for _,mode:=range []string{"success","denied","changed"}{t.Run(mode,func(t *testing.T){
        r,p:=fixture(t);socket(t,r,p);originalSocket:=r.path(p);before,_:=os.ReadFile(filepath.Join(r.Store.Root,"devices.json"));slot:="";stopped:=false
        r.Run=func(ctx context.Context,args,env []string,terminal bool)error{
            path:="";for i,arg:=range args{if arg=="-S"{path=args[i+1]}}
            if path==originalSocket{t.Fatal("recovery touched original master")}
            joined:=strings.Join(args," ")
            if strings.Contains(joined,"-O stop"){stopped=true;return nil}
            if strings.Contains(joined,"-O check"){return nil}
            for _,value:=range env{if strings.HasPrefix(value,"JANUS_SSH_RECOVERY_SLOT="){slot=strings.TrimPrefix(value,"JANUS_SSH_RECOVERY_SLOT=")}}
            if len(slot)!=64{t.Fatal("missing fresh recovery slot")}
            current,_:=r.Store.Get(p.ID);if current.CredentialSlot!=""{t.Fatal("selected before SSH verification")}
            if mode=="denied"{return errors.New("authentication rejected")}
            listener,err:=net.Listen("unix",path);if err!=nil{t.Fatal(err)};t.Cleanup(func(){listener.Close()})
            if mode=="changed"{if err:=r.Store.Rename(p.ID,"changed");err!=nil{t.Fatal(err)}}
            return nil
        }
        err:=r.RecoverCredential(context.Background(),p);current,_:=r.Store.Get(p.ID)
        if mode=="success"{if err!=nil||current.CredentialSlot!=slot||!stopped{t.Fatal(current,err,stopped)}}else{if err==nil||current.CredentialSlot!=""{t.Fatal("failed recovery selected new credential",current,err)}}
        if mode=="denied"{after,_:=os.ReadFile(filepath.Join(r.Store.Root,"devices.json"));if string(before)!=string(after){t.Fatal("failed recovery changed registry")}}
        if _,err:=os.Lstat(originalSocket);err!=nil{t.Fatal("original session socket changed",err)}
    })}
}
