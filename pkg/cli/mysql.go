package cli

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "strings"
    "time"

    "github.com/spf13/cobra"
    "golang.org/x/term"
    "github.com/leo1394/homebrew-janus/internal/database"
    "github.com/leo1394/homebrew-janus/internal/cliprovider"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

func mysqlAccount(s store.Store,p janus.Profile)(store.Account,error){return s.Account(p.AccountID)}
func connectMySQL(cmd *cobra.Command,r database.Runtime,p janus.Profile)error{
    a,err:=mysqlAccount(r.Store,p);if err!=nil{return err};path,err:=r.Ensure(cmd.Context(),p,a,false);if err!=nil{return err};return mysqlProfileREPL(cmd,r,p,path)
}
func spawnMySQL(cmd *cobra.Command,r database.Runtime,args []string)error{
    var port int;var name,ca,tlsMode string
    sub:=&cobra.Command{Use:"mysql USER@HOST [flags]",Short:"Register and connect to a MySQL database; verify TLS by default.",Example:"  janus spawn mysql user@db.example.com -p 3306\n  janus spawn mysql user@db.example.com --ca /path/to/ca.pem\n  janus spawn mysql user@db.example.com --tls disabled",Args:cobra.ExactArgs(1),SilenceUsage:true,SilenceErrors:true,RunE:func(sub *cobra.Command,args []string)error{
        at:=strings.LastIndex(args[0],"@");if at<=0{return errors.New("expected USER@HOST")};user,host:=args[0][:at],strings.Trim(args[0][at+1:],"[]")
        pem:="";if ca!=""{f,err:=os.Open(ca);if err!=nil{return err};data,err:=io.ReadAll(io.LimitReader(f,65537));f.Close();if err!=nil||len(data)>65536{return errors.New("CA file is too large or unreadable")};pem=string(data)}
        if err:=database.ValidateTarget(database.Target{Host:host,User:user,Port:port,Database:name,CAPEM:pem,TLSMode:tlsMode});err!=nil{return err}
        p,a,pending,err:=r.Store.PrepareMySQL(user,host,port,name,pem,tlsMode);if err!=nil{return err}
        path,err:=r.Ensure(sub.Context(),p,a,pending);if err!=nil{return err};return mysqlProfileREPL(sub,r,p,path)
    }}
    sub.Flags().IntVarP(&port,"port","p",3306,"MySQL port")
    sub.Flags().StringVar(&name,"database","","Optional initial database; omit to connect without selecting one")
    sub.Flags().StringVar(&tlsMode,"tls","verify","TLS mode: verify or disabled (unencrypted connection)")
    sub.Flags().StringVar(&ca,"ca","","PEM CA certificate; defaults to system trust")
    sub.SetIn(cmd.InOrStdin());sub.SetOut(cmd.OutOrStdout());sub.SetErr(cmd.ErrOrStderr());sub.SetArgs(args);return sub.ExecuteContext(cmd.Context())
}
func mysqlProfileREPL(cmd *cobra.Command,r database.Runtime,p janus.Profile,path string)error{
    ctx,cancel:=context.WithCancel(cmd.Context());defer cancel()
    if !mysqlProfileFresh(r.Store,p){go func(){if r.RefreshProfile(ctx,p,path,"","")==nil{_ = refreshAllDatabases(ctx,r.Store,r,p,path)}}()}
    return mysqlREPL(cmd,path)
}
func mysqlREPL(cmd *cobra.Command,path string)error{
    c,err:=database.Dial(cmd.Context(),path);if err!=nil{return err};defer c.Close()
    reply,err:=c.Call(cmd.Context(),database.Request{Method:"open"});if err!=nil{return err};if reply.Session==nil{return errors.New("missing SQL session")};id:=reply.Session.ID
    fmt.Fprintln(cmd.ErrOrStderr(),"MySQL session. End SQL with ';'. Use \\q to exit. Statements are never automatically retried.")
    scanner:=bufio.NewScanner(cmd.InOrStdin());scanner.Buffer(make([]byte,4096),256<<10);var statement strings.Builder
    interactive:=false;if f,ok:=cmd.InOrStdin().(*os.File);ok{interactive=term.IsTerminal(int(f.Fd()))}
    for{
        if interactive{prompt:="mysql> ";if statement.Len()>0{prompt="    -> "};fmt.Fprint(cmd.ErrOrStderr(),prompt)}
        if !scanner.Scan(){break}
        line:=scanner.Text();if statement.Len()==0&&(strings.TrimSpace(line)=="\\q"||strings.TrimSpace(line)=="exit"){return nil}
        statement.WriteString(line);statement.WriteByte('\n');if statement.Len()>256<<10{return errors.New("SQL statement exceeds 256 KiB")}
        if !sqlComplete(statement.String()){continue}
        reply,err:=c.Call(cmd.Context(),database.Request{Method:"query",SessionID:id,SQL:statement.String()});statement.Reset()
        if err!=nil{fmt.Fprintln(cmd.ErrOrStderr(),err);continue}
        if reply.Result!=nil{if err:=json.NewEncoder(cmd.OutOrStdout()).Encode(reply.Result);err!=nil{return err}}
    }
    if err:=scanner.Err();err!=nil{return err};if strings.TrimSpace(statement.String())!=""{return errors.New("incomplete SQL; terminate the statement with ';'")};return nil
}
// A terminator must be outside quoted literals and comments. SQL is never split
// and replayed: the driver independently rejects multiple statements.
func sqlComplete(s string)bool{
    var quote byte;line,block:=false,false;last:=byte(0)
    for i:=0;i<len(s);i++{c:=s[i]
        if line{if c=='\n'{line=false};continue};if block{if c=='*'&&i+1<len(s)&&s[i+1]=='/'{block=false;i++};continue}
        if quote!=0{if c=='\\'{i++;continue};if c==quote{if i+1<len(s)&&s[i+1]==quote{i++}else{quote=0}};continue}
        if c=='#'{line=true;continue};if c=='-'&&i+2<len(s)&&s[i+1]=='-'&&s[i+2]<=' '{line=true;i++;continue};if c=='/'&&i+1<len(s)&&s[i+1]=='*'{block=true;i++;continue}
        if c=='\''||c=='"'||c=='`'{quote=c;last=c;continue};if c>' '{last=c}
    };return quote==0&&!block&&last==';'
}
func addMySQLCommands(root *cobra.Command,r database.Runtime){
    root.AddCommand(&cobra.Command{Use:"__mysql-owner",Hidden:true,Args:cobra.NoArgs,RunE:func(cmd *cobra.Command,args []string)error{var start database.Startup;decoder:=json.NewDecoder(io.LimitReader(cmd.InOrStdin(),100<<10));decoder.DisallowUnknownFields();if err:=decoder.Decode(&start);err!=nil{return errors.New("invalid database startup")};return r.RunOwner(cmd.Context(),start)}})
    session:=&cobra.Command{Use:"session",Short:"Manage local authenticated sessions."}
    var idleTimeout time.Duration
    serve:=&cobra.Command{Use:"serve",Short:"Serve connection-bound CLI provider approval and identity requests over stdio.",Args:cobra.NoArgs,RunE:func(cmd *cobra.Command,args []string)error{return cliprovider.ServeWithIdleTimeout(cmd.Context(),r.Store,r.Namespace,cmd.InOrStdin(),cmd.OutOrStdout(),idleTimeout)}}
    serve.Flags().DurationVar(&idleTimeout,"idle-timeout",15*time.Minute,"Idle authorization timeout (5m to 1h); successful operations reset it")
    session.AddCommand(serve)
    session.AddCommand(&cobra.Command{Use:"list",Short:"List managed MySQL and CLI provider sessions as JSON.",Args:cobra.NoArgs,RunE:func(cmd *cobra.Command,args []string)error{return databaseSessionCommand(cmd,r,"list","")}})
    for _,method:=range []string{"close","revoke"}{method:=method;session.AddCommand(&cobra.Command{Use:method+" SESSION_ID",Short:"Stop a managed SQL session and cancel its pending operation.",Args:cobra.ExactArgs(1),RunE:func(cmd *cobra.Command,args []string)error{return databaseSessionCommand(cmd,r,method,args[0])}})}
    root.AddCommand(session)
}
