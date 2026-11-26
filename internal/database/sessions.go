package database

import (
    "context"
    "crypto/rand"
    "database/sql"
    "encoding/hex"
    "errors"
    "fmt"
    "sync"
    "time"

    mysql "github.com/go-sql-driver/mysql"
)

type SessionInfo struct {
    ID string `json:"id"`
    DeviceID string `json:"deviceId,omitempty"`
    AccountID string `json:"accountId,omitempty"`
    Provider string `json:"provider,omitempty"`
    State string `json:"state"`
    LastUsed time.Time `json:"lastUsed"`
}

type sqlSession struct {
    mu sync.Mutex
    info SessionInfo
    conn *sql.Conn
    cancel context.CancelFunc
    closed bool
}

type Sessions struct {
    mu sync.Mutex
    db *sql.DB
    binding string
    deviceID string
    accepting bool
    authorize func()error
    admit func()error
    sessions map[string]*sqlSession
}

func NewSessions(db *sql.DB,deviceID string)*Sessions{return &Sessions{db:db,deviceID:deviceID,accepting:true,sessions:map[string]*sqlSession{}}}
func(s *Sessions)Open(ctx context.Context)(SessionInfo,error){
    if s.authorize!=nil{if err:=s.authorize();err!=nil{return SessionInfo{},err}}
    if s.admit!=nil{if err:=s.admit();err!=nil{return SessionInfo{},err}}
    s.mu.Lock();defer s.mu.Unlock()
    if !s.accepting{return SessionInfo{},errors.New("database is disconnected; new sessions are disabled")}
    if len(s.sessions)>=8{return SessionInfo{},errors.New("database session limit reached")}
    conn,err:=s.db.Conn(ctx);if err!=nil{return SessionInfo{},errors.New("cannot open database session")}
    var id [16]byte;if _,err:=rand.Read(id[:]);err!=nil{conn.Close();return SessionInfo{},err}
    info:=SessionInfo{ID:hex.EncodeToString(id[:]),DeviceID:s.deviceID,State:"active",LastUsed:time.Now()}
    s.sessions[info.ID]=&sqlSession{info:info,conn:conn}
    return info,nil
}
func(s *Sessions)Disconnect(){s.mu.Lock();s.accepting=false;s.mu.Unlock()}
func(s *Sessions)Accepting()bool{s.mu.Lock();defer s.mu.Unlock();return s.accepting}
func(s *Sessions)List()[]SessionInfo{
    s.mu.Lock();rows:=make([]*sqlSession,0,len(s.sessions));for _,v:=range s.sessions{rows=append(rows,v)};s.mu.Unlock()
    out:=make([]SessionInfo,0,len(rows));for _,v:=range rows{v.mu.Lock();out=append(out,v.info);v.mu.Unlock()};return out
}
func(s *Sessions)Close(id string)error{
    s.mu.Lock();v:=s.sessions[id];s.mu.Unlock();if v==nil{return errors.New("unknown database session")}
    v.mu.Lock();if v.closed{v.mu.Unlock();return nil};v.closed=true;v.info.State="closed";if v.cancel!=nil{v.cancel()};v.mu.Unlock()
    // Conn.Close waits for an admitted operation; cancellation runs first.
    err:=v.conn.Close()
    s.mu.Lock();delete(s.sessions,id);s.mu.Unlock()
    if err!=nil{return errors.New("database session cleanup failed")};return nil
}
func(s *Sessions)Shutdown(){s.Disconnect();for _,v:=range s.List(){_ = s.Close(v.ID)};s.db.Close()}
func(s *Sessions)Expire(before time.Time){for _,v:=range s.List(){if v.LastUsed.Before(before){_ = s.Close(v.ID)}}}

type Result struct {
    Columns []string `json:"columns"`
    Rows [][]*string `json:"rows"`
}

// Query never retries. A transport failure may follow a committed server write;
// callers must not silently replay the SQL. Multi-statements are driver-disabled.
func(s *Sessions)Query(ctx context.Context,id,statement string)(Result,error){
    if s.authorize!=nil{if err:=s.authorize();err!=nil{return Result{},err}}
    if len(statement)==0||len(statement)>256<<10{return Result{},errors.New("SQL statement is empty or too large")}
    s.mu.Lock();v:=s.sessions[id];s.mu.Unlock();if v==nil{return Result{},errors.New("unknown database session")}
    v.mu.Lock()
    if v.closed||v.cancel!=nil{v.mu.Unlock();return Result{},errors.New("database session is closed or busy")}
    request,cancel:=context.WithTimeout(ctx,60*time.Second);v.cancel=cancel;v.mu.Unlock()
    defer func(){cancel();v.mu.Lock();v.cancel=nil;v.info.LastUsed=time.Now();v.mu.Unlock()}()
    rows,err:=v.conn.QueryContext(request,statement)
    if err!=nil{var serverError *mysql.MySQLError;if errors.As(err,&serverError){return Result{},fmt.Errorf("MySQL error %d; statement was not retried",serverError.Number)};_ = s.Close(id);return Result{},errors.New("SQL failed or connection interrupted; execution outcome may be unknown; statement was not retried")}
    defer rows.Close()
    columns,err:=rows.Columns();if err!=nil{return Result{},errors.New("cannot read query columns")}
    if len(columns)>1024{return Result{},errors.New("query has too many columns")}
    result:=Result{Columns:columns,Rows:[][]*string{}};size:=0
    for rows.Next(){
        if len(result.Rows)>=1000{return Result{},errors.New("query result exceeds 1000 rows; add a LIMIT")}
        values:=make([]sql.NullString,len(columns));dest:=make([]any,len(columns));for i:=range values{dest[i]=&values[i]}
        if err:=rows.Scan(dest...);err!=nil{return Result{},errors.New("cannot decode query result")}
        row:=make([]*string,len(columns));for i,value:=range values{if value.Valid{value:=value.String;size+=len(value);row[i]=&value}}
        if size>1<<20{return Result{},errors.New("query result exceeds 1 MiB")};result.Rows=append(result.Rows,row)
    }
    if err:=rows.Err();err!=nil{rows.Close();_ = s.Close(id);return Result{},errors.New("query interrupted; partial results discarded; statement was not retried")}
    return result,nil
}
