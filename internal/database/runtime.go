package database

import (
    "bufio"
    "bytes"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "sync"
    "syscall"
    "time"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

type Runtime struct{Store store.Store;Namespace string}
type Startup struct{Profile janus.Profile `json:"profile"`;Account store.Account `json:"account"`;Pending bool `json:"pending"`;Enroll bool `json:"enroll"`;Socket string `json:"socket"`}
type Client struct{conn net.Conn;decoder *json.Decoder;mu sync.Mutex}
func Dial(ctx context.Context,path string)(*Client,error){
    parent,err:=os.Lstat(filepath.Dir(path));if err!=nil{return nil,err};stat,ok:=parent.Sys().(*syscall.Stat_t);if !ok||!parent.IsDir()||parent.Mode().Perm()&0077!=0||stat.Uid!=uint32(os.Getuid()){return nil,errors.New("unsafe database socket directory")}
    info,err:=os.Lstat(path);if err!=nil{return nil,err};if info.Mode()&os.ModeSocket==0||info.Mode().Perm()&0077!=0{return nil,errors.New("unsafe database socket")}
    conn,err:=(&net.Dialer{Timeout:time.Second}).DialContext(ctx,"unix",path);if err!=nil{return nil,err}
    if !sameUser(conn.(*net.UnixConn)){conn.Close();return nil,errors.New("database owner identity mismatch")}
    return &Client{conn:conn,decoder:json.NewDecoder(conn)},nil
}
func(c *Client)Close(){c.conn.Close()}
func(c *Client)Call(ctx context.Context,q Request)(Response,error){
    c.mu.Lock();defer c.mu.Unlock();q.ID="request"
    deadline:=time.Now().Add(65*time.Second);if d,ok:=ctx.Deadline();ok&&d.Before(deadline){deadline=d};c.conn.SetDeadline(deadline)
    stopped:=make(chan struct{});finished:=make(chan struct{});go func(){defer close(finished);select{case <-ctx.Done():c.conn.SetDeadline(time.Now());case <-stopped:}}();defer func(){close(stopped);<-finished}()
    if err:=json.NewEncoder(c.conn).Encode(q);err!=nil{return Response{},errors.New("database owner disconnected; operation was not retried")}
    var response Response;if err:=c.decoder.Decode(&response);err!=nil{return response,errors.New("database owner response lost; operation outcome may be unknown")}
    if response.ID!=q.ID{return response,errors.New("invalid owner response")};if response.Error!=""{return response,errors.New(response.Error)};return response,nil
}
func(r Runtime)directory()string{return filepath.Join(r.Store.Root,"mysql")}
func(r Runtime)sockets()([]string,error){return filepath.Glob(filepath.Join(r.directory(),"*.sock"))}
func(r Runtime)Find(ctx context.Context,p janus.Profile)(string,error){
    paths,err:=r.sockets();if err!=nil{return "",err}
    for _,path:=range paths{c,err:=Dial(ctx,path);if err!=nil{continue};reply,err:=c.Call(ctx,Request{Method:"status"});c.Close();if err==nil&&reply.Accepting&&reply.DeviceID==p.ID&&reply.Binding==Binding(p){return path,nil}}
    return "",nil
}
func Binding(p janus.Profile)string{p.Name="";b,_:=json.Marshal(p);return string(b)}
func(r Runtime)Connected(ctx context.Context,p janus.Profile)(bool,error){path,err:=r.Find(ctx,p);return path!="",err}
func(r Runtime)Disconnect(ctx context.Context,p janus.Profile)error{
    paths,err:=r.sockets();if err!=nil{return err}
    for _,path:=range paths{c,err:=Dial(ctx,path);if err!=nil{continue};reply,err:=c.Call(ctx,Request{Method:"status"});if err==nil&&reply.DeviceID==p.ID{_,err=c.Call(ctx,Request{Method:"disconnect"})};c.Close();if err!=nil{return err}}
    return nil
}
func(r Runtime)Ensure(ctx context.Context,p janus.Profile,a store.Account,pending bool)(string,error){
    unlock,err:=r.Store.Lock("mysql-owner");if err!=nil{return "",err};defer unlock()
    if !pending{current,err:=r.Store.Get(p.ID);if err!=nil||current!=p{return "",errors.New("database target changed; select again")};path,err:=r.Find(ctx,p);if err!=nil||path!=""{return path,err}}
    if err:=store.SecureDir(r.directory());err!=nil{return "",err}
    var token [12]byte;if _,err:=rand.Read(token[:]);err!=nil{return "",err}
    path:=filepath.Join(r.directory(),hex.EncodeToString(token[:])+".sock");if len(path)>100{return "",errors.New("database data directory is too long for a Unix socket")}
    accounts,err:=r.Store.Accounts();if err!=nil{return "",err};enroll:=true;for _,saved:=range accounts{if saved.ID==a.ID{enroll=false}}
    input,_:=json.Marshal(Startup{Profile:p,Account:a,Pending:pending,Enroll:enroll,Socket:path})
    executable,err:=os.Executable();if err!=nil{return "",err}
    cmd:=exec.Command(executable,"__mysql-owner");cmd.Stdin=bytes.NewReader(input);cmd.Stderr=os.Stderr;cmd.SysProcAttr=&syscall.SysProcAttr{Setsid:true}
    ready,err:=cmd.StdoutPipe();if err!=nil{return "",err}
    if err:=cmd.Start();err!=nil{return "",err}
    done:=make(chan error,1);go func(){done<-cmd.Wait()}()
    result:=make(chan string,1);go func(){scanner:=bufio.NewScanner(ready);if scanner.Scan(){result<-scanner.Text()}else{result<-""}}()
    select{
    case line:=<-result:
        if line!="ready"{_ = cmd.Process.Kill();return "",errors.New("database approval or connection failed; device was not registered")}
        return path,nil
    case <-ctx.Done():_ = cmd.Process.Kill();return "",ctx.Err()
    case <-time.After(210*time.Second):_ = cmd.Process.Kill();return "",errors.New("database approval timed out")
    }
}
func(r Runtime)RunOwner(ctx context.Context,start Startup)error{
    p:=start.Profile
    if !p.IsMySQL()||store.Validate(p)!=nil||filepath.Dir(start.Socket)!=r.directory()||!strings.HasSuffix(start.Socket,".sock"){return errors.New("invalid database startup")}
    if start.Account.ID!=p.AccountID||start.Account.Host!=p.Host||start.Account.Port!=p.Port||start.Account.User!=p.User{return errors.New("database account binding mismatch")}
    if !start.Enroll{account,err:=r.Store.Account(start.Account.ID);if err!=nil||account!=start.Account{return errors.New("database account changed")}}
    if !start.Pending{saved,err:=r.Store.Get(p.ID);if err!=nil||saved!=p{return errors.New("database target changed")}}
    if err:=ValidateTarget(Target{Host:p.Host,Port:p.Port,User:p.User,Database:p.Database,CAPEM:p.DatabaseCA,TLSMode:p.DatabaseTLS});err!=nil{return err}
    secret,err:=Password(ctx,start.Account,r.Namespace,start.Enroll,p.DatabaseTLS);if err!=nil{return err}
    db,err:=Open(ctx,Target{Host:p.Host,Port:p.Port,User:p.User,Database:p.Database,CAPEM:p.DatabaseCA,TLSMode:p.DatabaseTLS},secret);for i:=range secret{secret[i]=0};if err!=nil{return err}
    defer db.Close()
    if start.Pending{p,err=r.Store.RegisterMySQL(p,start.Account);if err!=nil{return err}}else{current,err:=r.Store.Get(p.ID);if err!=nil||current!=p{return errors.New("database changed during authentication")}}
    listener,err:=net.ListenUnix("unix",&net.UnixAddr{Name:start.Socket,Net:"unix"});if err!=nil{return err};defer listener.Close();defer os.Remove(start.Socket)
    if err:=os.Chmod(start.Socket,0600);err!=nil{return err}
    currentAccount,err:=r.Store.Account(start.Account.ID);if err!=nil{return err};expected:=start.Account;expected.Name=currentAccount.Name;if currentAccount!=expected{return errors.New("account changed during authentication")};start.Account=currentAccount
    sessions:=NewSessions(db,p.ID);sessions.binding=Binding(p)
    sessions.authorize=func()error{account,err:=r.Store.Account(start.Account.ID);if err!=nil||account!=start.Account{return errors.New("database account authorization was revoked")};return nil}
    sessions.admit=func()error{current,err:=r.Store.Get(p.ID);if err!=nil||Binding(current)!=Binding(p){return errors.New("database target changed or was removed")};return nil}
    if _,err:=fmt.Fprintln(os.Stdout,"ready");err!=nil{return err}
    // Detach readiness output; the long-lived owner never prints query data.
    os.Stdout.Close()
    owner,cancel:=context.WithCancel(ctx);defer cancel()
    go func(){ticker:=time.NewTicker(time.Second);defer ticker.Stop();idle:=time.Now();for{select{case <-owner.Done():return;case <-ticker.C:account,err:=r.Store.Account(start.Account.ID);if err!=nil||account!=start.Account{cancel();return};if sessions.admit()!=nil{sessions.Disconnect()};if len(sessions.List())>0{idle=time.Now()}else if !sessions.Accepting()||time.Since(idle)>15*time.Minute{cancel();return}}}}()
    return Serve(owner,listener,sessions)
}
