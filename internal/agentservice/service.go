// Package agentservice keeps one authenticated A2X owner connection alive.
// Unix permissions separate interfaces, not hostile processes with the same UID.
package agentservice

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "net"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "regexp"
    "sync"
    "syscall"
    "time"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/internal/a2x"
    "golang.org/x/sys/unix"
)

type Remote interface { Call(context.Context,string,any,any)error; Done()<-chan struct{} }
type Request struct { InstanceID string `json:"instanceId,omitempty"`; Method string `json:"method"`; Params json.RawMessage `json:"params"`; GrantID string `json:"grantId,omitempty"`; Credential string `json:"credential,omitempty"` }
type Response struct { Code int `json:"code,omitempty"`; OK bool `json:"ok"`; Result json.RawMessage `json:"result,omitempty"`; Error string `json:"error,omitempty"` }
type Credential struct { LocalOwnerUID *uint32 `json:"localOwnerUid,omitempty"`; GrantID string `json:"grantId"`; Credential string `json:"credential"`; InstanceID string `json:"instanceId"` }
func decode(data []byte,out any)error{
    d:=json.NewDecoder(bytes.NewReader(data));d.DisallowUnknownFields();if err:=d.Decode(out);err!=nil{return errors.New("invalid parameters")};var extra any;if d.Decode(&extra)!=io.EOF{return errors.New("trailing JSON")};return nil
}
var profilePattern=regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
func validText(s string,max int)bool{return strings.TrimSpace(s)!=""&&len(s)<=max&&!strings.ContainsRune(s,0)}
// Validation is duplicated locally for fail-closed routing; the remote server is
// authoritative for grants, expiry, actor task ownership and concurrency quotas.
func Validate(q Request,owner bool)error{
    if !owner&&(!validText(q.GrantID,256)||!validText(q.Credential,4096)){return errors.New("delegated credentials required")}
    if owner&&(q.GrantID!=""||q.Credential!=""){return errors.New("owner endpoint does not accept delegated credentials")}
    if !owner&&IsA2AMethod(q.Method){var p map[string]json.RawMessage;if json.Unmarshal(q.Params,&p)!=nil||p==nil{return errors.New("params must be an object")};return nil}
    switch q.Method{
    case "agent.card":if owner{break};var p struct{};return decode(q.Params,&p)
    case "a2x.events":if owner{break};var p struct{ID string `json:"id"`;After int64 `json:"after"`};if decode(q.Params,&p)!=nil||!validText(p.ID,256)||p.After<0{return errors.New("invalid event cursor")};return nil
    case "agent.status","tasks.list":var p struct{};return decode(q.Params,&p)
    case "tasks.get":var p struct{TaskID string `json:"taskId"`};if decode(q.Params,&p)!=nil||!validText(p.TaskID,256){return errors.New("taskId required")};return nil
    case "tasks.submit":
        if owner{return errors.New("task submission requires a delegation")}
        var p struct{IdempotencyKey string `json:"idempotencyKey"`;Text string `json:"text"`;Profile string `json:"profile"`}
        if decode(q.Params,&p)!=nil||!validText(p.IdempotencyKey,256)||!validText(p.Text,256<<10)||!profilePattern.MatchString(p.Profile){return errors.New("invalid task profile or parameters")};return nil
    case "grant.create":
        if !owner{break};var p struct{ActorID string `json:"actorId"`;Profile string `json:"profile"`;TTLSeconds int `json:"ttlSeconds"`;MaxConcurrent int `json:"maxConcurrent"`}
        if decode(q.Params,&p)!=nil||!validText(p.ActorID,256)||!profilePattern.MatchString(p.Profile)||p.TTLSeconds<1||p.TTLSeconds>86400||p.MaxConcurrent<1||p.MaxConcurrent>8{return errors.New("invalid grant profile or parameters")};return nil
    case "grant.revoke":if !owner{break};var p struct{GrantID string `json:"grantId"`};if decode(q.Params,&p)!=nil||!validText(p.GrantID,256){return errors.New("grantId required")};return nil
    }
    return errors.New("method is not permitted on this endpoint")
}
func handle(ctx context.Context,remote Remote,instance string,owner bool,slots chan struct{})http.Handler{
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        w.Header().Set("Content-Type","application/json")
        reply:=func(value Response){json.NewEncoder(w).Encode(value)}
        if r.Method!="POST"||r.URL.Path!="/call"{reply(Response{Error:"unsupported endpoint"});return}
        body,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,1<<20));if err!=nil{reply(Response{Error:"request exceeds limit"});return}
        var q Request;err=decode(body,&q);for i:=range body{body[i]=0}
        if err!=nil{reply(Response{Error:"invalid request"});return}
        if !owner&&q.InstanceID!=instance{reply(Response{Error:"grant belongs to another Agenux instance"});return}
        if err:=Validate(q,owner);err!=nil{reply(Response{Error:err.Error()});return}
        select{case slots<-struct{}{}:defer func(){<-slots}();default:reply(Response{Error:"connection service busy"});return}
        method,params:=q.Method,any(q.Params)
        if !owner{method="delegated.call";params=struct{GrantID string `json:"grantId"`;Credential string `json:"credential"`;Method string `json:"method"`;Params json.RawMessage `json:"params"`}{q.GrantID,q.Credential,q.Method,q.Params}}
        request,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
        var result json.RawMessage
        if err:=remote.Call(request,method,params,&result);err!=nil{reply(Response{Code:a2x.SafeErrorCode(err),Error:"remote request rejected or connection unavailable"});return}
        reply(Response{OK:true,Result:result})
    })
}
func owned(path string,mode os.FileMode)error{
    info,err:=os.Lstat(path);if err!=nil{return err};stat,ok:=info.Sys().(*syscall.Stat_t)
    if !ok||stat.Uid!=uint32(os.Getuid())||info.Mode().Perm()&0077!=0||info.Mode()&os.ModeType!=mode{return errors.New("unsafe service file")};return nil
}
// Serve holds an exclusive directory lock and never replaces a running listener.
// No automatic reconnect: loss of the authenticated channel ends this service.
func Serve(ctx context.Context,dir,instance string,remote Remote,ready func())error{return ServeIsolated(ctx,dir,instance,remote,ServeOptions{},ready)}
func ServeIsolated(ctx context.Context,dir,instance string,remote Remote,options ServeOptions,ready func())error{
    if (options.DelegateDirectory=="")!=(len(options.DelegateUIDs)==0){return errors.New("delegate directory and allowed UIDs must be configured together")}
    ownerUID:=uint32(os.Getuid());for _,uid:=range options.DelegateUIDs{if uid==ownerUID||uid==0{return errors.New("delegate UID must be non-root and distinct from the owner")}}
    if options.DelegateDirectory!=""{ownerPath:=filepath.Clean(dir);delegatePath:=filepath.Clean(options.DelegateDirectory)
        if ownerPath==delegatePath||strings.HasPrefix(delegatePath,ownerPath+string(filepath.Separator))||strings.HasPrefix(ownerPath,delegatePath+string(filepath.Separator)){return errors.New("delegate and owner directories must be separate, not nested")};if err:=sharedDirectory(options.DelegateDirectory,ownerUID,true);err!=nil{return err}}
    if !filepath.IsAbs(dir){return errors.New("service directory must be absolute")}
    if err:=store.SecureDir(dir);err!=nil{return err}
    fd,err:=unix.Open(filepath.Join(dir,"service.lock"),unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC,0600);if err!=nil{return err}
    lock:=os.NewFile(uintptr(fd),"service.lock");defer lock.Close()
    if err:=owned(filepath.Join(dir,"service.lock"),0);err!=nil{return err}
    if err:=unix.Flock(fd,unix.LOCK_EX|unix.LOCK_NB);err!=nil{return errors.New("connection service already running")};defer unix.Flock(fd,unix.LOCK_UN)
    ctx,cancel:=context.WithCancel(ctx);defer cancel()
    slots:=make(chan struct{},8);servers:=[]*http.Server{};var workers sync.WaitGroup
    defer func(){cancel();for _,server:=range servers{server.Close()};workers.Wait()}()
    failures:=make(chan error,3)
    type endpoint struct{directory,name string;uids []uint32;mode os.FileMode}
    endpoints:=[]endpoint{{dir,"owner.sock",[]uint32{ownerUID},0600},{dir,"delegated.sock",[]uint32{ownerUID},0600}}
    if options.DelegateDirectory!=""{endpoints=append(endpoints,endpoint{options.DelegateDirectory,"delegated.sock",options.DelegateUIDs,0666})}
    for _,endpoint:=range endpoints{
        name:=endpoint.name;path:=filepath.Join(endpoint.directory,name)
        if info,err:=os.Lstat(path);err==nil{
            stat,ok:=info.Sys().(*syscall.Stat_t);if !ok||info.Mode()&os.ModeType!=os.ModeSocket||stat.Uid!=ownerUID{return errors.New("unsafe stale socket")}
            if endpoint.mode==0666{probe,e:=net.DialTimeout("unix",path,200*time.Millisecond);if e==nil{probe.Close();return errors.New("delegate socket already in use")}}
            if err=os.Remove(path);err!=nil{return err}
        }else if !os.IsNotExist(err){return err}
        listener,err:=net.Listen("unix",path);if err!=nil{return err}
        if err:=os.Chmod(path,endpoint.mode);err!=nil{listener.Close();return err}
        server:=&http.Server{Handler:restrictUIDs(handle(ctx,remote,instance,name=="owner.sock",slots),endpoint.uids),ConnContext:peerContext,ReadTimeout:5*time.Second,WriteTimeout:35*time.Second,IdleTimeout:5*time.Second,MaxHeaderBytes:4096};servers=append(servers,server)
        workers.Add(1);go func(){defer workers.Done();failures<-server.Serve(listener)}()
    }
    if ready!=nil{ready()}
    select{case <-ctx.Done():return nil;case <-remote.Done():return errors.New("authenticated agent connection closed; restart service with owner approval");case err:=<-failures:return err}
}
func Call(ctx context.Context,dir string,owner bool,q Request)(Response,error){return call(ctx,dir,owner,q,nil)}
func call(ctx context.Context,dir string,owner bool,q Request,pinnedUID *uint32)(Response,error){
    var result Response
    if !filepath.IsAbs(dir){return result,errors.New("service directory must be absolute")}
    if pinnedUID==nil{if err:=owned(dir,os.ModeDir);err!=nil{return result,err}}else{if err:=delegatedSocket(dir,*pinnedUID);err!=nil{return result,err}}
    name:="delegated.sock";if owner{name="owner.sock"};path:=filepath.Join(dir,name)
    if pinnedUID==nil{if err:=owned(path,os.ModeSocket);err!=nil{return result,err}}
    data,err:=json.Marshal(q);if err!=nil{return result,err};defer func(){for i:=range data{data[i]=0}}()
    transport:=&http.Transport{DialContext:func(ctx context.Context,_,_ string)(net.Conn,error){conn,err:=(&net.Dialer{}).DialContext(ctx,"unix",path);if err!=nil{return nil,err}
        expected:=uint32(os.Getuid());if pinnedUID!=nil{expected=*pinnedUID};uid,err:=peerUID(conn);if err!=nil||uid!=expected{conn.Close();return nil,errors.New("service peer UID does not match pinned owner")};return conn,nil}};defer transport.CloseIdleConnections()
    client:=&http.Client{Transport:transport,Timeout:40*time.Second}
    request,err:=http.NewRequestWithContext(ctx,"POST","http://janus/call",bytes.NewReader(data));if err!=nil{return result,err}
    response,err:=client.Do(request);if err!=nil{return result,errors.New("local connection service unavailable")};defer response.Body.Close()
    body,err:=io.ReadAll(io.LimitReader(response.Body,(4<<20)+1));if err!=nil||len(body)>4<<20{return result,errors.New("invalid service response")}
    if err:=decode(body,&result);err!=nil{return result,err};return result,nil
}
func WriteCredential(path string,value Credential)error{
    if value.GrantID==""||value.Credential==""||value.InstanceID==""{return errors.New("incomplete grant response")}
    if err:=owned(filepath.Dir(path),os.ModeDir);err!=nil{return err}
    fd,err:=unix.Open(path,unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC,0600);if err!=nil{return err}
    file:=os.NewFile(uintptr(fd),path);err=json.NewEncoder(file).Encode(value);if err==nil{err=file.Sync()};closeErr:=file.Close();if err==nil{err=closeErr};if err!=nil{os.Remove(path)};return err
}
func ReadCredential(path string)(Credential,error){
    var c Credential
    fd,err:=unix.Open(path,unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC,0);if err!=nil{return c,err};f:=os.NewFile(uintptr(fd),path);defer f.Close()
    info,err:=f.Stat();if err!=nil{return c,err};stat,ok:=info.Sys().(*syscall.Stat_t);if !ok||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0||stat.Uid!=uint32(os.Getuid())||info.Size()>8192{return c,errors.New("unsafe credential file")}
    data,err:=io.ReadAll(io.LimitReader(f,8193));if err!=nil{return c,err};defer func(){for i:=range data{data[i]=0}}();if err:=decode(data,&c);err!=nil{return c,err};if !validText(c.Credential,4096)||!validText(c.GrantID,256)||!validText(c.InstanceID,256){return c,errors.New("invalid credential file")};return c,nil
}

func IsA2AMethod(method string)bool{
    switch method{case "SendMessage","SendStreamingMessage","GetTask","ListTasks","CancelTask","SubscribeToTask","CreateTaskPushNotificationConfig","GetTaskPushNotificationConfig","ListTaskPushNotificationConfigs","DeleteTaskPushNotificationConfig","GetExtendedAgentCard":return true};return false
}
