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
    devices := &cobra.Command{Use:"devices",Short:"List saved SSH and AI agent devices.",Long:"List device IDs, names, platform, type, reusable-connection and network status. Default: --list. JSON uses null for unknown. Listing may perform bounded network probes but never reads credentials or starts a master.",Args:cobra.NoArgs,Example:"  janus devices\n  janus devices --json"}
    var asJSON, asList bool
    devices.Flags().BoolVar(&asJSON,"json",false,"Print JSON (null means unknown)")
    devices.Flags().BoolVar(&asList,"list",false,"Print the table (default)")
    devices.MarkFlagsMutuallyExclusive("json","list")
    devices.RunE = func(cmd *cobra.Command,args []string) error {
        rows,err:=s.List();if err!=nil{return err}
        result:=make([]janus.Device,0,len(rows))
        deadline,cancel:=context.WithTimeout(cmd.Context(),600*time.Millisecond);defer cancel()
        for _,p:=range rows {
            live,err:=r.Connected(deadline,p);if err!=nil{return err}
            connected:=live
            d:=janus.Device{ID:displayID(p.ID),Name:p.Name,Platform:p.DevicePlatform(),Type:p.DeviceType(),Connected:&connected}
            if live { yes:=true;d.Ready=&yes } else if deadline.Err()==nil {
                dial:=net.Dialer{Timeout:100*time.Millisecond};conn,err:=dial.DialContext(deadline,"tcp",net.JoinHostPort(p.Host,strconv.Itoa(p.Port)))
                ready:=err==nil;if conn!=nil{conn.Close()};if deadline.Err()==nil{d.Ready=&ready}
            }
            result=append(result,d)
        }
        for _,provider:=range options.Providers{extra,err:=provider.Devices(cmd.Context());if err!=nil{return err};result=append(result,extra...)}
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
        for _,p:=range rows{candidates=append(candidates,selector.Candidate{Name:p.Name,Path:displayID(p.ID),Detail:store.Target(p)})}
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
        sort.Strings(out);return out,cobra.ShellCompDirectiveNoFileComp
    }
    adapters:=[]deviceConnectionAdapter{
        {platform:"terminal",kind:"ssh",connect:func(cmd *cobra.Command,p janus.Profile)error{
            if emptyAgent || shareMCP!="" {return errors.New("--empty and --share-mcp require an AI agent device")}
            timing.Mark("runtime_ensure_start");err:=r.Ensure(cmd.Context(),p);timing.Mark("runtime_ensure_complete");if err!=nil{return err};return r.Attach(cmd.Context(),p)
        }},
        {platform:"a2x",kind:"",connect:func(cmd *cobra.Command,p janus.Profile)error{return connectAgent(cmd,s,r,p,emptyAgent,shareMCP)}},
    }
    connectCommand:=&cobra.Command{Use:"connect [ID|NAME]",Short:"Connect using the saved device's platform and type.",Long:"Open an SSH terminal or Agenux agent session through the device connection adapter. With no target, select from all devices; --empty filters the selector to AI agents and creates a new session. Agents resume a sole available session, always show a selector for multiple sessions, or create one when none exists.",Args:cobra.MaximumNArgs(1),Example:"  janus connect\n  janus connect server-name\n  janus connect --empty",ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{
        p,err:=selectProfile(cmd,args);if err!=nil{return err};p,err=s.Get(p.ID);if err!=nil{return err}
        return connectDevice(cmd,p,adapters)
    }}
    connectCommand.Flags().BoolVar(&emptyAgent,"empty",false,"Create a new agent session")
    connectCommand.Flags().StringVar(&shareMCP,"share-mcp","","Explicit local MCP stdio server and tool allowlist JSON (agent only)")
    connectCommand.RegisterFlagCompletionFunc("share-mcp",completeShareFile)
    root.AddCommand(connectCommand)
    root.AddCommand(agentMachineCommand(s,r))
    root.AddCommand(&cobra.Command{Use:"recover-credential ID|NAME",Short:"Enroll and verify a separate SSH credential, preserving the old item.",Args:cobra.ExactArgs(1),ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{
        rows,err:=s.List();if err!=nil{return err};p,err:=resolve(rows,args[0]);if err!=nil{return err}
        recovery,ok:=r.(interface{RecoverCredential(context.Context,janus.Profile)error});if !ok{return errors.New("runtime does not support credential recovery")}
        if err:=recovery.RecoverCredential(cmd.Context(),p);err!=nil{return err}
        fmt.Fprintln(cmd.OutOrStdout(),"Credential verified and selected. Previous Keychain item and active sessions are preserved.");return nil
    }})
    root.AddCommand(&cobra.Command{Use:"disconnect [ID|NAME ...]",Short:"Stop master reuse, preserving existing sessions.",Long:"Stop the selected SSH masters accepting new sessions. Existing sessions continue. With no devices, show the multi-select picker. Unmatched devices and operation failures do not stop the batch; print a summary and return a nonzero exit status if any fail.",Args:cobra.ArbitraryArgs,Example:"  janus disconnect\n  janus disconnect server-one server-two",ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{
        return runDeviceBatch(cmd,s,args,func(p janus.Profile)error{
            current,err:=s.Get(p.ID);if err!=nil{return err};return r.Disconnect(cmd.Context(),current)
        })
    }})
    root.AddCommand(&cobra.Command{Use:"remove [ID|NAME ...]",Short:"Delete devices and their Keychain credentials; keep active connections.",Long:"Delete selected devices and their exact SSH Keychain credentials. Active connections remain. With no devices, show the multi-select picker. Unmatched devices and operation failures do not stop the batch. If credential deletion fails, retain that device for retry. Print a summary and return a nonzero exit status if any fail.",Args:cobra.ArbitraryArgs,Example:"  janus remove\n  janus remove server-one server-two",ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{
        return runDeviceBatch(cmd,s,args,func(p janus.Profile)error{
            unlock,err:=s.Lock(displayID(p.ID));if err!=nil{return err};defer unlock()
            return s.Remove(p.ID,func(p janus.Profile)error{return r.DeleteCredential(cmd.Context(),p)})
        })
    }})
    root.AddCommand(&cobra.Command{Use:"rename ID|NAME new_NAME",Short:"Rename a device; names must be unique (case-insensitive).",Long:"Change only the saved display name. Duplicate names fail without mutation; existing sessions and credentials are unchanged. Quote names containing spaces.",Args:cobra.ExactArgs(2),Example:"  janus rename server-name 'Build Server'",ValidArgsFunction:completeDevice,RunE:func(cmd *cobra.Command,args []string)error{p,err:=selectProfile(cmd,args[:1]);if err!=nil{return err};return s.Rename(p.ID,args[1])}})
    spawn:=&cobra.Command{Use:"spawn [SSH arguments]",Short:"Register or reuse an SSH device.",Long:"Pass SSH arguments directly, without an extra ssh word. Resolve the destination using system SSH configuration, register its user/host/port, and open a session through the Janus Confidential CLI master. Passwords are handled by the native Keychain helper.",DisableFlagParsing:true,Example:"  janus spawn user@example.com -p 3233\n  janus spawn agent user@example.com -p 7263",ValidArgsFunction:completeSpawn}
    spawn.RunE=func(cmd *cobra.Command,args []string)error{
        if len(args)==0 || (len(args)==1 && (args[0]=="--help"||args[0]=="-h")){return cmd.Help()}
        if args[0]=="agent" { return spawnAgent(cmd,s,r,args[1:]) }
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
func displayID(id string)string{return strings.TrimPrefix(strings.TrimPrefix(id,"terminal:"),"agent:")}
func resolve(rows []janus.Profile,q string)(janus.Profile,error){
    for _,p:=range rows{if strings.EqualFold(p.ID,q)||strings.EqualFold(displayID(p.ID),q){return p,nil}}
    for _,p:=range rows{if strings.EqualFold(p.Name,q){return p,nil}}
    return janus.Profile{},fmt.Errorf("unknown device %q; run 'janus devices'",q)
}
func status(value *bool,color bool)string{
    if value==nil{return "-"};mark:="✘";code:="31";if *value{mark="✔︎";code="32"};if color{return "\x1b["+code+"m"+mark+"\x1b[0m"};return mark
}
