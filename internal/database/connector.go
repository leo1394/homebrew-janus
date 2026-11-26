// Package database owns authenticated SQL connections. Secrets must never cross
// the public session interface or be included in errors and diagnostics.
package database

import (
    "context"
    "crypto/tls"
    "crypto/x509"
    "database/sql"
    "errors"
    "io"
    "log"
    "net"
    "strconv"
    "time"

    mysql "github.com/go-sql-driver/mysql"
)

type Target struct {
    Host string `json:"host"`
    Port int `json:"port"`
    User string `json:"user"`
    Database string `json:"database"`
    TLSMode string `json:"tlsMode,omitempty"`
    CAPEM string `json:"caPEM,omitempty"`
}

// Open is called only by the trusted owner process after native approval.
// A CA is frozen into the approved target, not loaded from a mutable path later.
func Open(ctx context.Context,target Target,password []byte)(*sql.DB,error){
    roots,err:=targetRoots(target);if err!=nil{return nil,err}
    config:=mysql.NewConfig()
    config.Logger=log.New(io.Discard,"",0)
    config.Net="tcp";config.Addr=net.JoinHostPort(target.Host,strconv.Itoa(target.Port))
    config.User=target.User;config.Passwd=string(password);config.DBName=target.Database
    if target.TLSMode!="disabled"{config.TLS=&tls.Config{MinVersion:tls.VersionTLS12,ServerName:target.Host,RootCAs:roots}}
    config.Timeout=15*time.Second;config.ReadTimeout=60*time.Second;config.WriteTimeout=15*time.Second
    config.AllowAllFiles=false;config.AllowCleartextPasswords=false;config.MultiStatements=false
    connector,err:=mysql.NewConnector(config);if err!=nil{return nil,errors.New("invalid database configuration")}
    db:=sql.OpenDB(connector)
    // Idle connection state must not leak across interactive sessions. Each
    // session owns a dedicated connection until close; no cross-session reuse.
    db.SetMaxIdleConns(0);db.SetMaxOpenConns(8)
    if err:=db.PingContext(ctx);err!=nil{db.Close();return nil,errors.New("database connection failed; check TLS, network and account authentication")}
    return db,nil
}

func ValidateTarget(target Target)error{_,err:=targetRoots(target);return err}
func targetRoots(target Target)(*x509.CertPool,error){
    if target.Host==""||target.User==""||target.Port<1||target.Port>65535{return nil,errors.New("invalid database target")}
    if target.TLSMode!=""&&target.TLSMode!="verify"&&target.TLSMode!="disabled"{return nil,errors.New("invalid TLS mode")};if target.TLSMode=="disabled"&&target.CAPEM!=""{return nil,errors.New("CA cannot be used with disabled TLS")}
    roots,err:=x509.SystemCertPool();if err!=nil{roots=x509.NewCertPool()}
    if target.CAPEM!=""{roots=x509.NewCertPool();if len(target.CAPEM)>65536||!roots.AppendCertsFromPEM([]byte(target.CAPEM)){return nil,errors.New("invalid database CA certificate")}}
    return roots,nil
}
