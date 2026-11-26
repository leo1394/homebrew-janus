package database

import (
    "bufio"
    "bytes"
    "io"
    "context"
    "encoding/json"
    "errors"
    "net"
    "os"
    "sync"
    "time"

    "github.com/leo1394/homebrew-janus/internal/deviceprofile"
)

type Request struct {
    ID string `json:"id"`
    Method string `json:"method"`
    SessionID string `json:"sessionId,omitempty"`
    SQL string `json:"sql,omitempty"`
    Database string `json:"database,omitempty"`
    Table string `json:"table,omitempty"`
}
type Response struct {
    ID string `json:"id"`
    DeviceID string `json:"deviceId"`
    Binding string `json:"binding,omitempty"`
    Error string `json:"error,omitempty"`
    Session *SessionInfo `json:"session,omitempty"`
    Sessions []SessionInfo `json:"sessions,omitempty"`
    Result *Result `json:"result,omitempty"`
    Profile *deviceprofile.Section `json:"profile,omitempty"`
    Accepting bool `json:"accepting"`
}

// Serve exposes an owner-only local interface, not an Agent delegation API.
// Session identifiers do not authorize SQL: execution is bound to the connection
// which opened the session. Future Agent entry points must enforce explicit scopes.
func Serve(ctx context.Context,listener *net.UnixListener,sessions *Sessions)error{
    ctx,cancel:=context.WithCancel(ctx)
    var workers sync.WaitGroup
    stop:=make(chan struct{});defer close(stop)
    go func(){select{case <-ctx.Done():listener.Close();case <-stop:}}()
    ticker:=time.NewTicker(time.Minute);defer ticker.Stop()
    go func(){for{select{case <-ticker.C:sessions.Expire(time.Now().Add(-15*time.Minute));case <-stop:return}}}()
    defer func(){cancel();sessions.Shutdown();workers.Wait()}()
    for{
        conn,err:=listener.AcceptUnix();if err!=nil{if ctx.Err()!=nil{return nil};return err}
        if !sameUser(conn){conn.Close();continue}
        workers.Add(1);go func(){defer workers.Done();serveConnection(ctx,conn,sessions)}()
    }
}
func sameUser(conn *net.UnixConn)bool{
    raw,err:=conn.SyscallConn();if err!=nil{return false}
    var uid uint32;var peerErr error
    err=raw.Control(func(fd uintptr){uid,peerErr=socketPeerUID(int(fd))})
    return err==nil&&peerErr==nil&&uid==uint32(os.Getuid())
}
func serveConnection(ctx context.Context,conn *net.UnixConn,sessions *Sessions){
    defer conn.Close()
    done:=make(chan struct{});defer close(done)
    go func(){select{case <-ctx.Done():conn.Close();case <-done:}}()
    owned:=map[string]bool{};defer func(){for id:=range owned{_ = sessions.Close(id)}}()
    scanner:=bufio.NewScanner(conn);scanner.Buffer(make([]byte,4096),300<<10)
    encoder:=json.NewEncoder(conn)
    for{
        conn.SetReadDeadline(time.Now().Add(15*time.Minute))
        if !scanner.Scan(){return}
        var q Request
        decoder:=json.NewDecoder(bytes.NewReader(scanner.Bytes()));decoder.DisallowUnknownFields()
        if decoder.Decode(&q)!=nil||q.ID==""||len(q.ID)>128{return};var extra any;if decoder.Decode(&extra)!=io.EOF{return}
        reply:=Response{ID:q.ID,Accepting:sessions.Accepting(),DeviceID:sessions.deviceID,Binding:sessions.binding};var err error
        switch q.Method{
        case "status":
        case "open":
            var info SessionInfo;info,err=sessions.Open(ctx);if err==nil{owned[info.ID]=true;reply.Session=&info}
        case "query":
            if !owned[q.SessionID]{err=errors.New("SQL session belongs to another connection");break}
            var result Result;result,err=sessions.Query(ctx,q.SessionID,q.SQL);if err==nil{reply.Result=&result}
        case "profile":
            section,_:=sessions.CollectProfile(ctx,q.Database,q.Table);reply.Profile=&section
        case "list":reply.Sessions=sessions.List()
        case "disconnect":sessions.Disconnect();reply.Accepting=false
        case "close","revoke":err=sessions.Close(q.SessionID);delete(owned,q.SessionID)
        default:err=errors.New("unsupported database operation")
        }
        if err!=nil{reply.Error=err.Error()}
        conn.SetWriteDeadline(time.Now().Add(15*time.Second))
        if encoder.Encode(reply)!=nil{return}
    }
}

// SameUser validates the kernel-reported peer identity for owner control sockets.
func SameUser(conn *net.UnixConn)bool{return sameUser(conn)}
