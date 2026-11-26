package cli

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "sort"
    "strings"
    "time"

    "github.com/spf13/cobra"
    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/leo1394/homebrew-janus/internal/database"
    "github.com/leo1394/homebrew-janus/internal/deviceprofile"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

type sshProfileCollector interface{CollectProfile(context.Context,janus.Profile)error}
func addProfileCommands(root *cobra.Command,s store.Store,r janus.SSHRuntime,db database.Runtime,selectDevice func(*cobra.Command,[]string)(janus.Profile,error),complete func(*cobra.Command,[]string,string)([]string,cobra.ShellCompDirective)){
    var refresh,asJSON,showAll bool;var databaseName,table string
    command:=&cobra.Command{Use:"profile [ID|NAME]",Short:"View a cached device profile or explicitly refresh it.",Long:"Show locally cached SSH system information, agent capabilities or MySQL metadata. Cached observations are descriptive, not authorization. Reading does not connect or request credentials. --refresh uses the normal connection approval when required. MySQL table details are collected only when explicitly selected; no business rows are read.",Args:cobra.MaximumNArgs(1),Example:"  janus profile\n  janus profile server-name --refresh\n  janus profile database-name --database app --table orders --refresh\n  janus profile database-name --show-all --refresh\n  janus profile server-name --json",RunE:func(cmd *cobra.Command,args []string)error{
        if showAll&&(databaseName!=""||table!=""){return errors.New("--show-all cannot be combined with --database or --table")}
        if table!=""&&databaseName==""{return errors.New("--table requires --database")}
        if len(databaseName)>64||len(table)>64{return errors.New("database and table names must be at most 64 bytes")}
        p,err:=selectDevice(cmd,args);if err!=nil{return err}
        if (showAll||databaseName!=""||table!="")&&!p.IsMySQL(){return errors.New("database profile options require a MySQL device")}
        if refresh{
            switch{
            case p.IsMySQL():
                account,err:=s.Account(p.AccountID);if err!=nil{return err};path,err:=db.Ensure(cmd.Context(),p,account,false);if err!=nil{return err}
                if err:=db.RefreshProfile(cmd.Context(),p,path,databaseName,table);err!=nil{return err}
                if showAll{if err:=refreshAllDatabases(cmd.Context(),s,db,p,path);err!=nil{return err}}
            case p.IsAgent():
                p,err=refreshAgentProfile(cmd.Context(),s,r,p);if err!=nil{return err}
            default:
                collector,ok:=r.(sshProfileCollector);if !ok{return errors.New("runtime does not support SSH profiles")}
                if err:=r.Ensure(cmd.Context(),p);err!=nil{return err};if err:=collector.CollectProfile(cmd.Context(),p);err!=nil{return err}
            }
        }
        snapshot,err:=deviceprofile.Read(s,p);if os.IsNotExist(err){return errors.New("no cached profile; connect to the device or run janus profile ID --refresh")};if err!=nil{return err}
        if databaseName!=""{key:=database.ProfileKey(databaseName,table);section,ok:=snapshot.Sections[key];if !ok{return errors.New("metadata not cached; repeat with --refresh")};snapshot.Sections=map[string]deviceprofile.Section{key:section}}
        if showAll{snapshot,err=allDatabaseProfiles(snapshot);if err!=nil{return err}}
        if asJSON{return json.NewEncoder(cmd.OutOrStdout()).Encode(snapshot)}
        fmt.Fprintf(cmd.OutOrStdout(),"%s (%s/%s)\nCached observations only; not an access grant.\n",deviceprofile.SafeText(snapshot.Name),snapshot.Platform,snapshot.Type)
        keys:=[]string{};for key:=range snapshot.Sections{keys=append(keys,key)};sort.Strings(keys)
        for _,key:=range keys{section:=snapshot.Sections[key];freshness:="fresh";if section.Status=="failed"||time.Now().After(section.ExpiresAt){freshness="stale"};fmt.Fprintf(cmd.OutOrStdout(),"\n%s — %s, %s\nSource: %s\nObserved: %s\n",deviceprofile.SafeText(key),section.Status,freshness,section.Source,section.ObservedAt.Format(time.RFC3339));if section.Error!=""{fmt.Fprintln(cmd.OutOrStdout(),deviceprofile.SafeText(section.Error))};if len(section.Data)>0{var value any;if json.Unmarshal(section.Data,&value)==nil{data,err:=json.MarshalIndent(value,"","  ");if err!=nil{return err};for _,line:=range strings.Split(string(data),"\n"){if _,err:=fmt.Fprintln(cmd.OutOrStdout(),deviceprofile.SafeText(line));err!=nil{return err}}}}}
        return nil
    }}
    command.ValidArgsFunction=complete
    command.Flags().BoolVar(&refresh,"refresh",false,"Collect through an authorized connection; prompt if required")
    command.Flags().BoolVar(&showAll,"show-all",false,"Show all visible database profiles; --refresh also collects their table directories")
    command.Flags().BoolVar(&asJSON,"json",false,"Print cached observations and timestamps as JSON")
    command.Flags().StringVar(&databaseName,"database","","MySQL database whose table directory to inspect")
    command.Flags().StringVar(&table,"table","","MySQL table whose columns, indexes and foreign keys to inspect")
    root.AddCommand(command)
    root.AddCommand(&cobra.Command{Use:"__profile-ssh ID",Hidden:true,Args:cobra.ExactArgs(1),RunE:func(cmd *cobra.Command,args []string)error{
        p,err:=s.Get(args[0]);if err!=nil{return err};if p.IsAgent()||p.IsMySQL(){return errors.New("not an SSH device")}
        collector,ok:=r.(sshProfileCollector);if !ok{return errors.New("profile collector unavailable")};return collector.CollectProfile(cmd.Context(),p)
    }})
}
func refreshAgentProfile(ctx context.Context,s store.Store,r janus.SSHRuntime,p janus.Profile)(janus.Profile,error){
    transport,ok:=r.(agentTransport);if !ok{return p,errors.New("agent transport unavailable")}
    stream,err:=transport.Agenux(ctx,p);if err!=nil{return p,err};defer stream.Close()
    client,err:=a2x.New(stream,nil,nil);if err!=nil{return p,err};defer client.Close()
    if err:=authenticateAgent(ctx,client,r,p,p.InstanceName);err!=nil{return p,err}
    request,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel();var info deviceprofile.AgentInfo
    if err:=client.Call(request,"agent.info",struct{}{},&info);err!=nil{
        _ = deviceprofile.Merge(s,p,map[string]deviceprofile.Section{"overview":deviceprofile.Failure("a2x.agent.info","agent profile discovery unavailable")})
        return p,errors.New("agent profile discovery unavailable; server must support agent.info")
    }
    p,err=s.RegisterAuthenticatedAgent(p,false,info.InstanceID,info.Type,"");if err!=nil{return p,err}
    return p,deviceprofile.Agent(s,p,info,client.Capabilities().Terminal)
}

func visibleDatabases(snapshot deviceprofile.Snapshot)([]string,error){
    section,ok:=snapshot.Sections["overview"];if !ok{return nil,errors.New("database list not cached; repeat with --refresh")}
    var data map[string]database.Result;if json.Unmarshal(section.Data,&data)!=nil{return nil,errors.New("invalid cached database list")}
    result,ok:=data["databases"];if !ok{return nil,errors.New("database list not cached; repeat with --refresh")}
    names:=[]string{};seen:=map[string]bool{}
    for _,row:=range result.Rows{if len(row)!=1||row[0]==nil||*row[0]==""||len(*row[0])>64{return nil,errors.New("invalid cached database name")};name:=*row[0];if !seen[name]{names=append(names,name);seen[name]=true}}
    sort.Strings(names);return names,nil
}
func allDatabaseProfiles(snapshot deviceprofile.Snapshot)(deviceprofile.Snapshot,error){
    names,err:=visibleDatabases(snapshot);if err!=nil{return snapshot,err}
    sections:=map[string]deviceprofile.Section{"overview":snapshot.Sections["overview"]}
    for _,name:=range names{section,ok:=snapshot.Sections[database.ProfileKey(name,"")];if !ok{section=deviceprofile.Section{Source:"mysql.information_schema",Status:"not-collected",Error:"Table directory not cached; repeat with --refresh"}};sections["database: "+name]=section}
    snapshot.Sections=sections;return snapshot,nil
}
func refreshAllDatabases(ctx context.Context,s store.Store,r database.Runtime,p janus.Profile,path string)error{
    snapshot,err:=deviceprofile.Read(s,p);if err!=nil{return err};names,err:=visibleDatabases(snapshot);if err!=nil{return err}
    if len(names)>100{return errors.New("more than 100 databases; refresh selected databases individually")}
    request,cancel:=context.WithTimeout(ctx,60*time.Second);defer cancel();failed:=0
    for _,name:=range names{if request.Err()!=nil{return errors.New("database profile refresh timed out; completed observations retained")};if err:=r.RefreshProfile(request,p,path,name,"");err!=nil{failed++}}
    if failed>0{return fmt.Errorf("%d of %d database profiles failed; inspect cached profiles for details",failed,len(names))};return nil
}

func mysqlProfileFresh(s store.Store,p janus.Profile)bool{
    snapshot,err:=deviceprofile.Read(s,p);if err!=nil{return false}
    fresh:=func(key string)bool{section,ok:=snapshot.Sections[key];return ok&&section.Status=="ok"&&time.Now().Before(section.ExpiresAt)}
    if !fresh("overview"){return false};names,err:=visibleDatabases(snapshot);if err!=nil{return false}
    for _,name:=range names{if !fresh(database.ProfileKey(name,"")){return false}};return true
}
