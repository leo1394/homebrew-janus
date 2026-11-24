package agentservice

import (
    "context"
    "errors"
    "net"
    "net/http"
    "os"
    "path/filepath"
    "syscall"
)

type ServeOptions struct { DelegateDirectory string; DelegateUIDs []uint32 }
type peerIdentity struct{UID uint32;Valid bool}
type peerIdentityKey struct{}
func peerUID(conn net.Conn)(uint32,error){
    unixConn,ok:=conn.(*net.UnixConn);if !ok{return 0,errors.New("Unix peer identity required")}
    raw,err:=unixConn.SyscallConn();if err!=nil{return 0,err}
    var uid uint32;var failure error
    if err:=raw.Control(func(fd uintptr){uid,failure=socketPeerUID(int(fd))});err!=nil{return 0,err};return uid,failure
}
func peerContext(ctx context.Context,conn net.Conn)context.Context{
    uid,err:=peerUID(conn);return context.WithValue(ctx,peerIdentityKey{},peerIdentity{uid,err==nil})
}
func restrictUIDs(next http.Handler,uids []uint32)http.Handler{
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        identity,_:=r.Context().Value(peerIdentityKey{}).(peerIdentity)
        if identity.Valid{for _,uid:=range uids{if identity.UID==uid{next.ServeHTTP(w,r);return}}}
        http.Error(w,"Unix peer UID is not authorized",http.StatusForbidden)
    })
}
// A shared IPC directory grants traversal, never mutation, to delegates. Reject
// writable/symlink ancestors, except root/owner-owned sticky temporary roots.
func sharedDirectory(path string,owner uint32,create bool)error{
    if !filepath.IsAbs(path)||filepath.Clean(path)!=path{return errors.New("delegate directory must be an absolute clean path")}
    if create{if err:=os.MkdirAll(path,0755);err!=nil{return err}}
    for current:=path;;current=filepath.Dir(current){
        info,err:=os.Lstat(current);if err!=nil{return err};stat,ok:=info.Sys().(*syscall.Stat_t)
        if !ok||!info.IsDir()||info.Mode()&os.ModeSymlink!=0{return errors.New("unsafe delegate directory")}
        if current==path{if stat.Uid!=owner||info.Mode().Perm()&0022!=0{return errors.New("delegate directory must be owned by the pinned service UID and not writable by delegates")}}else{
            if stat.Uid!=owner&&stat.Uid!=0{return errors.New("unsafe delegate directory ancestor owner")}
            if info.Mode().Perm()&0022!=0&&info.Mode()&os.ModeSticky==0{return errors.New("unsafe writable delegate directory ancestor")}
        }
        if current==filepath.Dir(current){break}
    }
    return nil
}
func delegatedSocket(dir string,owner uint32)error{
    if err:=sharedDirectory(dir,owner,false);err!=nil{return err}
    info,err:=os.Lstat(filepath.Join(dir,"delegated.sock"));if err!=nil{return err};stat,ok:=info.Sys().(*syscall.Stat_t)
    if !ok||info.Mode()&os.ModeType!=os.ModeSocket||stat.Uid!=owner{return errors.New("delegated socket owner does not match grant binding")};return nil
}
// CallCredential also verifies the connected server's kernel UID. A mount or
// socket substitution cannot redirect a credential to a different service UID.
func CallCredential(ctx context.Context,dir string,c Credential,method string,params []byte)(Response,error){
    owner:=uint32(os.Getuid());if c.LocalOwnerUID!=nil{owner=*c.LocalOwnerUID}
    q:=Request{InstanceID:c.InstanceID,GrantID:c.GrantID,Credential:c.Credential,Method:method,Params:params}
    if err:=Validate(q,false);err!=nil{return Response{},err}
    return call(ctx,dir,false,q,&owner)
}
