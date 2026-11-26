package cliprovider

import (
    "bufio"
    "bytes"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "io"
    "net"
    "os"
    "path/filepath"
    "sync"
    "time"

    "github.com/leo1394/homebrew-janus/internal/database"
    "github.com/leo1394/homebrew-janus/internal/store"
)
func Resource(a store.Account)string{return a.ID}
func Metadata(a store.Account)map[string]any{return map[string]any{"version":1,"resourceID":Resource(a),"environment":a.Environment,"system":"rea","username":a.User,"origin":origin}}
type request struct{Version int `json:"version"`;ID string `json:"requestID"`;Method string `json:"method"`;Parameters map[string]string `json:"parameters"`}
func decode(line []byte)(request,error){var q request;d:=json.NewDecoder(bytes.NewReader(line));d.DisallowUnknownFields();if d.Decode(&q)!=nil||q.Version!=1||!safe(q.ID,128,false){return q,ErrUnavailable};var extra any;if d.Decode(&extra)!=io.EOF{return q,ErrUnavailable};return q,nil}
func Serve(ctx context.Context,s store.Store,namespace string,input io.Reader,output io.Writer)error{return serve(ctx,s,namespace,input,output,database.Password,New)}
func ServeWithIdleTimeout(ctx context.Context,s store.Store,namespace string,input io.Reader,output io.Writer,idleTimeout time.Duration)error{
    if idleTimeout<5*time.Minute||idleTimeout>time.Hour{return errors.New("idle timeout must be between 5m and 1h")}
    return serve(ctx,s,namespace,input,output,database.Password,New,idleTimeout)
}
func serve(ctx context.Context,s store.Store,namespace string,input io.Reader,output io.Writer,password func(context.Context,store.Account,string,bool,...string)([]byte,error),newClient func(store.Account)*Client,idleOptions ...time.Duration)error{
    idleTimeout:=15*time.Minute;if len(idleOptions)>0{idleTimeout=idleOptions[0]}
    ctx,cancel:=context.WithCancel(ctx);defer cancel()
    lines:=make(chan []byte,1)
    go func(){scanner:=bufio.NewScanner(input);scanner.Buffer(make([]byte,4096),65536);defer close(lines);defer cancel();for scanner.Scan(){line:=append([]byte(nil),scanner.Bytes()...);select{case lines<-line:case <-ctx.Done():return}}}()
    var client *Client;defer func(){if client!=nil{client.Close()}}()
    var account store.Account;var id string;var listener *net.UnixListener
    var mu sync.Mutex;active:=false;last:=time.Now()
    defer func(){if listener!=nil{listener.Close()}}()
    ticker:=time.NewTicker(time.Second);defer ticker.Stop()
    go func(){for{select{case <-ctx.Done():return;case <-ticker.C:mu.Lock();live:=active;var a store.Account;if live{a=account};idle:=time.Since(last);mu.Unlock();if live{current,err:=s.Account(a.ID);if err!=nil||current!=a||idle>idleTimeout{cancel();return}}}}}()
    emit:=func(q request,response any)error{return json.NewEncoder(output).Encode(map[string]any{"version":1,"requestID":q.ID,"response":response})}
    for{select{case <-ctx.Done():return nil;case line,ok:=<-lines:
        if !ok{return nil};q,err:=decode(line);if err!=nil{return err}
        response:=map[string]any{"error":"unsupported_operation"}
        switch q.Method{
        case "session.request":
            if client!=nil||len(q.Parameters)!=1{break};resource:=q.Parameters["resourceID"]
            accounts,err:=s.Accounts();if err!=nil{return err};var found bool
            for _,a:=range accounts{if a.Provider=="clobotics"&&Resource(a)==resource{account=a;found=true;break}}
            if !found{response=map[string]any{"error":"resource_unavailable"};break}
            secret,err:=password(ctx,account,namespace,false);if err!=nil{response=map[string]any{"error":"approval_denied"};break}
            current,err:=s.Account(account.ID);if err!=nil||current!=account{for i:=range secret{secret[i]=0};return ErrUnavailable}
            client=newClient(account);err=client.Login(ctx,secret);for i:=range secret{secret[i]=0};if err!=nil{return ErrUnavailable}
            var token [16]byte;if _,err:=rand.Read(token[:]);err!=nil{return err};x:=hex.EncodeToString(token[:]);id=x[:8]+"-"+x[8:12]+"-"+x[12:16]+"-"+x[16:20]+"-"+x[20:]
            directory:=filepath.Join(s.Root,"cli");if err:=store.SecureDir(directory);err!=nil{return err};path:=filepath.Join(directory,x[:16]+".sock")
            listener,err=net.ListenUnix("unix",&net.UnixAddr{Name:path,Net:"unix"});if err!=nil{return errors.New("cannot open CLI provider control socket")};defer os.Remove(path);if err:=os.Chmod(path,0600);err!=nil{return err}
            mu.Lock();active=true;last=time.Now();mu.Unlock()
            go func(){<-ctx.Done();listener.Close()}()
            go func(){for{conn,err:=listener.AcceptUnix();if err!=nil{return};func(){defer conn.Close();if !database.SameUser(conn){return};for n:=0;n<16;n++{conn.SetDeadline(time.Now().Add(2*time.Second));var q database.Request;if json.NewDecoder(io.LimitReader(conn,4096)).Decode(&q)!=nil{return};reply:=database.Response{ID:q.ID};revoke:=false;mu.Lock();live:=active&&ctx.Err()==nil;used:=last;mu.Unlock();switch q.Method{case "list":reply.Sessions=[]database.SessionInfo{};if live{reply.Sessions=append(reply.Sessions,database.SessionInfo{ID:id,AccountID:account.ID,Provider:account.Provider,State:"active",LastUsed:used})};case "close","revoke":if q.SessionID!=id{reply.Error="unknown CLI provider session"}else{revoke=true};default:reply.Error="unsupported CLI provider control operation"};_ = json.NewEncoder(conn).Encode(reply);if revoke{cancel();return}}}()}}()
            response=map[string]any{"sessionID":id,"state":"active"}
        case "session.list":
            if len(q.Parameters)!=0{break};sessions:=[]any{};if client!=nil&&ctx.Err()==nil{sessions=append(sessions,map[string]any{"sessionID":id,"state":"active","resourceID":Resource(account),"accountRef":account.ID,"adapter":"clobotics","inFlight":0})};response=map[string]any{"sessions":sessions}
        case "session.execute":
            if client==nil||len(q.Parameters)!=3||q.Parameters["sessionID"]!=id||q.Parameters["operation"]!="inspect"||q.Parameters["subset"]!="identity"{break}
            current,err:=s.Account(account.ID);if err!=nil||current!=account{return ErrUnavailable};identity,err:=client.Inspect(ctx);if err!=nil{return ErrUnavailable};mu.Lock();last=time.Now();mu.Unlock();response=map[string]any{"result":identity}
        case "session.revoke":
            if client==nil||len(q.Parameters)!=1||q.Parameters["sessionID"]!=id{break};_ = emit(q,map[string]any{"revoked":true});return nil
        }
        if ctx.Err()!=nil{return nil};if err:=emit(q,response);err!=nil{return err}
    }}
}
