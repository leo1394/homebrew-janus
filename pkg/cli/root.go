package cli

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "os"
    "sort"
    "strconv"
    "strings"
    "text/tabwriter"
    "time"

    "github.com/spf13/cobra"
    "golang.org/x/term"
    "github.com/leo1394/homebrew-janus/internal/metadata"
    "github.com/leo1394/homebrew-janus/internal/database"
    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/internal/ssh"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/internal/timing"
    "github.com/leo1394/homebrew-janus/internal/upgrade"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

// Options is the composition boundary for a future Confidential CLI build.
// Register extensions explicitly in that build; no executable plugin discovery.
type Options struct {
    Store store.Store
    Runtime janus.SSHRuntime
    Providers []janus.DeviceProvider
    Extensions []func(*cobra.Command, janus.Runtime) error
}

func New(options Options) (*cobra.Command, error) {
    development,scope,borrowedSocket,err:=developmentEnvironment();if err!=nil{return nil,err}
    s := options.Store
    if development.Root!="" {if s.Root!=""||options.Runtime!=nil{return nil,errors.New("development environment conflicts with injected store/runtime")};s=development}
    if s.Root == "" { var err error; s, err = store.Default(); if err != nil { return nil, err } }
    r := options.Runtime
    if r == nil { r = &ssh.Runtime{Store:s,In:os.Stdin,Out:os.Stdout,Err:os.Stderr,CredentialNamespace:scope,AgentControlSocket:borrowedSocket} }
    dbRuntime:=database.Runtime{Store:s,Namespace:scope}
    root := &cobra.Command{Use:"janus",Short:"Manage SSH devices with Keychain credentials and reusable connections.",
        Long:"Janus Confidential CLI manages SSH devices, keeps passwords only in macOS Keychain, and reuses an authenticated master for new terminal sessions.",
        Example:"  janus spawn user@example.com -p 22\n  janus devices\n  janus connect\n  janus disconnect server-name",
        SilenceUsage:true,SilenceErrors:true,Version:metadata.Version}
    root.SetVersionTemplate(metadata.VersionText())
    root.SetFlagErrorFunc(flagError)
    root.CompletionOptions.DisableDefaultCmd = true
    version := &cobra.Command{Use:"version",Short:"Print the build version, release date and repository.",Args:cobra.NoArgs,Example:"  janus version",RunE:func(cmd *cobra.Command,args []string)error{_,err:=io.WriteString(cmd.OutOrStdout(),metadata.VersionText());return err}}
    root.AddCommand(version)
    root.AddCommand(&cobra.Command{Use:"__manpage",Hidden:true,Args:cobra.NoArgs,RunE:func(cmd *cobra.Command,args []string)error{_,err:=io.WriteString(cmd.OutOrStdout(),Manpage());return err}})
    devices := &cobra.Command{Use:"devices",Short:"List saved SSH, MySQL and AI agent devices.",Long:"List device IDs, names, platform, type, reusable-connection and network status. Default: --list. JSON uses null for unknown. Listing may perform bounded network probes but never reads credentials or starts a master.",Args:cobra.NoArgs,Example:"  janus devices\n  janus devices --json"}
    var asJSON, asList bool
    devices.Flags().BoolVar(&asJSON,"json",false,"Print JSON (null means unknown)")
    devices.Flags().BoolVar(&asList,"list",false,"Print the table (default)")
    devices.MarkFlagsMutuallyExclusive("json","list")
    devices.RunE = func(cmd *cobra.Command,args []string) error {
        rows,err:=s.List();if err!=nil{return err}
        result:=make([]janus.Device,0,len(rows))
        deadline,cancel:=context.WithTimeout(cmd.Context(),600*time.Millisecond);defer cancel()
        for _,p:=range rows {
            var live bool;var err error;if p.IsMySQL(){live,err=dbRuntime.Connected(deadline,p)}else{live,err=r.Connected(deadline,p)};if err!=nil{return err}
            connected:=live
            d:=janus.Device{ID:displayID(p.ID),Name:p.Name,Platform:p.DevicePlatform(),Type:p.DeviceType(),Connected:&connected}
            if live { yes:=true;d.Ready=&yes } else if deadline.Err()==nil {
                dial:=net.Dialer{Timeout:100*time.Millisecond};conn,err:=dial.DialContext(deadline,"tcp",net.JoinHostPort(p.Host,strconv.Itoa(p.Port)))
                ready:=err==nil;if conn!=nil{conn.Close()};if deadline.Err()==nil{d.Ready=&ready}
            }
            result=append(result,d)
        }
        for _,provider:=range options.Providers{extra,err:=provider.Devices(cmd.Context());if err!=nil{return err};result=append(result,extra...)}
        sort.SliceStable(result,func(i,j int)bool{return janus.PlatformOrder(result[i].Platform)<janus.PlatformOrder(result[j].Platform)})
        if asJSON{return json.NewEncoder(cmd.OutOrStdout()).Encode(result)}
        if len(result)==0{fmt.Fprintln(cmd.OutOrStdout(),"No devices saved. Add one with: janus spawn USER@HOST [-p PORT]");return nil}
        // Pad uncoloured values before adding ANSI so columns remain aligned.
        var table strings.Builder
        w:=tabwriter.NewWriter(&table,0,4,2,' ',0)
        fmt.Fprintln(w,"ID\tNAME\tPLATFORM\tTYPE\tCONNECTED\tREADY")
        color:=false;if f,ok:=cmd.OutOrStdout().(*os.File);ok{_,noColor:=os.LookupEnv("NO_COLOR");color=term.IsTerminal(int(f.Fd()))&&!noColor&&os.Getenv("TERM")!="dumb"}
        for _,d:=range result{fmt.Fprintf(w,"%s\t%s\t%s\t%s\t%s\t%s\n",d.ID,d.Name,d.Platform,d.Type,strings.ReplaceAll(status(d.Connected,false),"\ufe0e",""),strings.ReplaceAll(status(d.Ready,false),"\ufe0e",""))}
        if err:=w.Flush();err!=nil{return err}
        // The text presentation selector has zero terminal width; add it after padding.
        text:=strings.ReplaceAll(table.String(),"✔","✔︎");if color{text=strings.NewReplacer("✔︎","\x1b[32m✔︎\x1b[0m","✘","\x1b[31m✘\x1b[0m").Replace(text)}
        _,err=io.WriteString(cmd.OutOrStdout(),text);return err
    }
    root.AddCommand(devices)
    var emptyAgent bool
    var shareMCP string
    selectProfile:=func(cmd *cobra.Command,args []string)(janus.Profile,error){
        timing.Mark("device_selection_start")
        defer timing.Mark("device_selection_complete")
        rows,err:=s.List();if err!=nil{return janus.Profile{},err}
        if len(args)>0{return resolve(rows,args[0])}
        if cmd.Name()=="connect" && emptyAgent { filtered:=[]janus.Profile{};for _,p:=range rows{if p.IsAgent(){filtered=append(filtered,p)}};rows=filtered }
        if len(rows)==0{return janus.Profile{},errors.New("no matching devices; use janus spawn or janus spawn agent")}
        candidates:=make([]selector.Candidate,0,len(rows))
        for _,p:=range rows{candidates=append(candidates,selector.Candidate{Name:p.Name,Path:displayID(p.ID),Detail:p.DeviceType()+" · "+store.Target(p)})}
        chosen,ok,err:=selector.SelectOne(cmd.Context(),os.Stdin,cmd.ErrOrStderr(),selector.Prompt{Title:"Select device",EmptySelectionNotice:"Select one device before confirming."},candidates)
        if err!=nil{return janus.Profile{},fmt.Errorf("device selector: %w; specify ID or NAME when non-interactive",err)}
        if !ok{return janus.Profile{},errors.New("device selection cancelled")}
        for _,p:=range rows{if displayID(p.ID)==chosen.Path{return p,nil}}
        return janus.Profile{},errors.New("selection no longer exists")
    }
    completeDevice:=func(cmd *cobra.Command,args []string,toComplete string)([]string,cobra.ShellCompDirective){
        if len(args)>0 && cmd.Name()!="disconnect" && cmd.Name()!="remove"{return nil,cobra.ShellCompDirectiveNoFileComp}
        rows,err:=s.List();if err!=nil{return nil,cobra.ShellCompDirectiveNoFileComp}
        out:=[]string{}
        for _,p:=range rows{if cmd.Name()=="connect" && emptyAgent && !p.IsAgent(){continue};for _,v:=range []string{displayID(p.ID),p.Name}{if strings.HasPrefix(strings.ToLower(v),strings.ToLower(toComplete)){out=append(out,v)}}}
        return out,cobra.ShellCompDirectiveNoFileComp
    }
    adapters:=[]deviceConnectionAdapter{
        {platform:"database",kind:"mysql",connect:func(cmd *cobra.Command,p janus.Profile)error{if emptyAgent||shareMCP!=""{return errors.New("Agent options cannot be used with MySQL")};return connectMySQL(cmd,dbRuntime,p)}},
        {platform:"terminal",kind:"ssh",connect:func(cmd *cobra.Command,p janus.Profile)error{
            if emptyAgent || shareMCP!="" {return errors.New("--empty and --share-mcp require an AI agent device")}
            timing.Mark("runtime_ensure_start");err:=r.Ensure(cmd.Context(),p);timing.Mark("runtime_ensure_complete");if err!=nil{return err};return r.Attach(cmd.Context(),p)
        }},
        {platform:"a2x",kind:"",connect:func(cmd *cobra.Command,p janus.Profile)error{return connectAgent(cmd,s,r,p,emptyAgent,shareMCP)}},
    }
    connectCommand:=&cobra.Command{Use:"connect [ID|NAME]",Short:"Connect using the saved device's platform and type.",Long:"Open an SSH terminal, MySQL SQL session or A2X agent session through the device connection adapter. With no target, select from all devices; --empty filters the selector to AI agents and creates a new session. Agents resume a sole available session, always show a selector for multiple sessions, or create one when none exists.",Args:cobra.MaximumNArgs(1),Example:"  janus connect\n  janus connect server-name\n  janus connect --empty",ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{
        p,err:=selectProfile(cmd,args);if err!=nil{return err};p,err=s.Get(p.ID);if err!=nil{return err}
        return connectDevice(cmd,p,adapters)
    }}
    connectCommand.Flags().BoolVar(&emptyAgent,"empty",false,"Create a new agent session")
    connectCommand.Flags().StringVar(&shareMCP,"share-mcp","","Explicit local MCP stdio server and tool allowlist JSON (agent only)")
    connectCommand.RegisterFlagCompletionFunc("share-mcp",completeShareFile)
    root.AddCommand(connectCommand)
    addProfileCommands(root,s,r,dbRuntime,selectProfile,completeDevice)
    root.AddCommand(agentMachineCommand(s,r))
    root.AddCommand(bridgeCommand(s,r))
    root.AddCommand(credentialCommand(s,r,dbRuntime))
    addMySQLCommands(root,dbRuntime)
    root.AddCommand(&cobra.Command{Use:"disconnect [ID|NAME ...]",Short:"Stop master reuse, preserving existing sessions.",Long:"Stop the selected SSH masters or MySQL owners accepting new sessions. Existing sessions continue. With no devices, show the multi-select picker. Unmatched devices and operation failures do not stop the batch; print a summary and return a nonzero exit status if any fail.",Args:cobra.ArbitraryArgs,Example:"  janus disconnect\n  janus disconnect server-one server-two",ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{
        return runDeviceBatch(cmd,s,args,func(p janus.Profile)error{
            current,err:=s.Get(p.ID);if err!=nil{return err};if current.IsMySQL(){return dbRuntime.Disconnect(cmd.Context(),current)};return r.Disconnect(cmd.Context(),current)
        })
    }})
    root.AddCommand(&cobra.Command{Use:"remove [ID|NAME ...]",Short:"Remove devices or saved accounts.",Long:"Remove devices or accounts by ID or unique name. Referenced accounts cannot be removed; remove their devices first. MySQL device removal retains its account. Account removal deletes its selected Keychain credential. SSH device removal preserves active connections. With no arguments, select devices and accounts. Unmatched targets and failures do not stop the batch.",Args:cobra.ArbitraryArgs,Example:"  janus remove\n  janus remove server-one account-name",ValidArgsFunction:func(cmd *cobra.Command,args []string,prefix string)([]string,cobra.ShellCompDirective){out,directive:=completeDevice(cmd,args,prefix);accounts,_:=s.Accounts();for _,a:=range accounts{if strings.HasPrefix(a.Name,prefix){out=append(out,a.Name)}};return out,directive},RunE:func(cmd *cobra.Command,args []string)error{
        rows,err:=s.List();if err!=nil{return err};accounts,err:=s.Accounts();if err!=nil{return err}
        for _,a:=range accounts{rows=append(rows,janus.Profile{ID:a.ID,Name:a.Name,Platform:"credential",Type:a.Provider,User:a.User,Host:a.Host,Port:a.Port})}
        return runTargetBatch(cmd,rows,args,func(p janus.Profile)error{
            if p.Platform=="credential"{return s.RemoveAccount(p.ID,func(a store.Account)error{return database.DeleteCredential(cmd.Context(),a,dbRuntime.Namespace)})}
            unlock,err:=s.Lock(displayID(p.ID));if err!=nil{return err};defer unlock()
            return s.Remove(p.ID,func(p janus.Profile)error{return r.DeleteCredential(cmd.Context(),p)})
        })
    }})
    root.AddCommand(&cobra.Command{Use:"rename ID|NAME new_NAME",Short:"Rename a device; names must be unique (case-insensitive).",Long:"Change only the saved display name. Duplicate names fail without mutation; existing sessions and credentials are unchanged. Quote names containing spaces.",Args:cobra.ExactArgs(2),Example:"  janus rename server-name 'Build Server'",ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{p,err:=selectProfile(cmd,args[:1]);if err!=nil{return err};return s.Rename(p.ID,args[1])}})
    spawn:=&cobra.Command{Use:"spawn [SSH options] USER@HOST\n  janus spawn mysql USER@HOST [-p PORT] [--database NAME] [--ca FILE] [--tls verify|disabled]\n  janus spawn agent [USER@HOST|TERMINAL_ID|NAME] [-p PORT] [--agent-transport subsystem|stdio] [--instance NAME] [--type NAME] [--share-mcp FILE]",Short:"Register or reuse an SSH, MySQL or A2X agent device.",Long:"Register or reuse a device and open a session.\n\nSSH: pass SSH arguments directly, without an extra ssh word. Reuse an authenticated SSH master for new terminal sessions.\nMySQL: use mysql USER@HOST to register an account and device after successful authentication, then enter an interactive SQL session. TLS is verified by default; use --ca for a custom CA or explicitly select --tls disabled for an unencrypted connection.\nAgent: use agent USER@HOST to connect to an A2X agent over SSH. Pass a terminal device ID or name, or omit the target to select an SSH terminal and reuse its account, host and port; an existing agent device follows connect.\n\nOptional SSH flags (OpenSSH passthrough):\n  -p PORT       SSH port (default: SSH configuration or 22)\n  -l USER       Login username\n  -i FILE       Identity file\n  -F FILE       SSH configuration file\n  -J HOST       Jump host\n  -o KEY=VALUE  SSH option; see ssh(1) for additional options\n\nOptional MySQL flags:\n  -p, --port PORT       MySQL port (default: 3306)\n  --database NAME       Initial database; omit to connect without selecting one\n  --ca FILE             PEM CA certificate (default: system trust)\n  --tls verify|disabled TLS mode (default: verify; disabled is unencrypted)\n\nOptional agent flags:\n  -p, --port PORT                    SSH port (default: selected terminal port, otherwise 7263)\n  --agent-transport subsystem|stdio  Transport (default: subsystem; saved devices unchanged)\n  --instance NAME                    Instance route; omit to discover and select\n  --type NAME                        Expected agent type (not an instance route)\n  --share-mcp FILE                   Explicit local MCP tool allowlist JSON\n\nPasswords are entered in the native dialog and stored only in macOS Keychain. Run janus spawn mysql --help or janus spawn agent --help for connection options.",DisableFlagParsing:true,Example:"  janus spawn user@example.com -p 3233\n  janus spawn mysql user@db.example.com -p 3306\n  janus spawn mysql user@db.example.com --ca /path/to/ca.pem\n  janus spawn agent\n  janus spawn agent user@example.com -p 7263",ValidArgsFunction:completeSpawn}
    spawn.RunE=func(cmd *cobra.Command,args []string)error{
        if len(args)==0 || (len(args)==1 && (args[0]=="--help"||args[0]=="-h")){return cmd.Help()}
        if args[0]=="agent" { return spawnAgent(cmd,s,r,args[1:]) }
        if args[0]=="mysql" { return spawnMySQL(cmd,dbRuntime,args[1:]) }
        native,ok:=r.(*ssh.Runtime);if !ok{return errors.New("the selected runtime does not support SSH argument passthrough")}
        return native.Spawn(cmd.Context(),args)
    }
    root.AddCommand(spawn)
    completion:=&cobra.Command{Use:"completion bash|zsh|fish",Hidden:true,Short:"Generate shell completion without querying devices.",Args:cobra.ExactValidArgs(1),ValidArgs:[]string{"bash","zsh","fish"},Example:"  janus completion zsh > ~/.zfunc/_janus",RunE:func(cmd *cobra.Command,args []string)error{
        switch args[0]{case "bash":_,err:=io.WriteString(cmd.OutOrStdout(),bashCompletion);return err;case "zsh":return root.GenZshCompletion(cmd.OutOrStdout());case "fish":return root.GenFishCompletion(cmd.OutOrStdout(),true)};return nil
    }}
    root.AddCommand(completion)
    root.AddCommand(upgrade.Command())
    for _,register:=range options.Extensions{if err:=register(root,r);err!=nil{return nil,err}}
    return root,nil
}
func displayID(id string)string{return strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(id,"terminal:"),"agent:"),"database:")}
func resolve(rows []janus.Profile,q string)(janus.Profile,error){
    for _,p:=range rows{if strings.EqualFold(p.ID,q)||strings.EqualFold(displayID(p.ID),q){return p,nil}}
    for _,p:=range rows{if strings.EqualFold(p.Name,q){return p,nil}}
    return janus.Profile{},fmt.Errorf("unknown device %q; run 'janus devices'",q)
}
func status(value *bool,color bool)string{
    if value==nil{return "-"};mark:="✘";code:="31";if *value{mark="✔︎";code="32"};if color{return "\x1b["+code+"m"+mark+"\x1b[0m"};return mark
}
