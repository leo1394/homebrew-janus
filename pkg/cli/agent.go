package cli

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/spf13/cobra"
    "github.com/spf13/pflag"
    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/leo1394/homebrew-janus/internal/deviceprofile"
    "github.com/leo1394/homebrew-janus/internal/mcpshare"
    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
type agentTransport interface { Agenux(context.Context,janus.Profile)(io.ReadWriteCloser,error) }
func safeAgentText(value string)string{return strings.Map(func(r rune)rune{if r=='\n'||r=='\t'||r>=32&&r!=127&&!(r>=128&&r<=159){return r};return -1},value)}
func connectAgent(cmd *cobra.Command,s store.Store,r janus.SSHRuntime,p janus.Profile,empty bool,share string)error{return connectAgentFlow(cmd,s,r,p,empty,share,false,"")}
func connectAgentFlow(cmd *cobra.Command,s store.Store,r janus.SSHRuntime,p janus.Profile,empty bool,share string,pending bool,expected string,expectedInstance ...string)error{
    transport,ok:=r.(agentTransport);if !ok{return errors.New("runtime does not support Agenux")}
    registered:=!pending
    if pending{defer func(){if !registered{if cleanup,ok:=r.(interface{DiscardPendingAgent(context.Context,janus.Profile)error});ok{ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel();cleanup.DiscardPendingAgent(ctx,p)}}}()}
    var stream io.ReadWriteCloser;var err error
    if pending{creator,ok:=r.(interface{AgenuxPending(context.Context,janus.Profile)(io.ReadWriteCloser,error)});if !ok{return errors.New("runtime does not support pending agent authentication")};stream,err=creator.AgenuxPending(cmd.Context(),p)}else{stream,err=transport.Agenux(cmd.Context(),p)}
    if err!=nil{return err};defer stream.Close()
    tools,closeTools,err:=mcpshare.Load(cmd.Context(),share,cmd.ErrOrStderr());if err!=nil{return err};defer closeTools()
    if len(tools)>0 {fmt.Fprint(cmd.ErrOrStderr(),"Sharing local MCP tools:");for _,tool:=range tools{fmt.Fprint(cmd.ErrOrStderr()," ",safeAgentText(tool.Name))};fmt.Fprintln(cmd.ErrOrStderr())}
    ttyIn,ttyOut,isTTY:=agentTTY(cmd)
    var mu sync.Mutex;var streamed strings.Builder
    client,err:=a2x.New(stream,tools,func(delta a2x.Delta){if isTTY{return};mu.Lock();defer mu.Unlock();streamed.WriteString(delta.Text);fmt.Fprint(cmd.OutOrStdout(),safeAgentText(delta.Text))});if err!=nil{return err};defer client.Close()
    p,info,_,err:=verifyConnectedAgent(cmd.Context(),s,r,p,client,pending,expected,expectedInstance...);if err!=nil{return err};registered=true
    if !client.Capabilities().Terminal&&info.Capabilities.DelegatedTasks{
        fmt.Fprintf(cmd.OutOrStdout(),"Success: authenticated %s (type=%s).\nThis agent supports machine tasks only; an interactive terminal is unavailable.\nStart a task connection with:\n  janus agent service %s --directory <PRIVATE_DIRECTORY>\n",safeAgentText(p.Name),p.DeviceType(),displayID(p.ID));return nil
    }
    if !client.Capabilities().Terminal&&p.DeviceType()!="pi"{return errors.New("agent registered, but no native terminal or delegated task capability was advertised")}
    if isTTY && !client.Capabilities().Terminal{return errors.New("server does not support native agent terminal UI; upgrade Agenux (non-TTY task.send remains available)")}
    session:=""
    if !empty {
        loading:=func(){};if isTTY{loading=startAgentLoading(ttyOut)}
        listing,stop:=context.WithTimeout(cmd.Context(),30*time.Second);sessions,err:=client.Sessions(listing);stop();loading();if err!=nil{return err}
        session,err=chooseAgentSession(sessions,p.LastSession,func(candidates []selector.Candidate)(selector.Candidate,bool,error){
            return selector.SelectOne(cmd.Context(),os.Stdin,cmd.ErrOrStderr(),selector.Prompt{Title:"Select agent session"},candidates)
        })
        if err!=nil{return err}
    }
    // Selection time is not part of the remote request deadline.
    loading:=func(){};if isTTY{loading=startAgentLoading(ttyOut)}
    opened,stop:=context.WithTimeout(cmd.Context(),30*time.Second)
    session,err=client.Open(opened,session);stop();loading();if err!=nil{return err}
    if err:=s.SetSession(p.ID,session);err!=nil{return err}
    if isTTY{return runAgentTerminal(cmd.Context(),client,ttyIn,ttyOut)}
    fmt.Fprintf(cmd.ErrOrStderr(),"Agent session %s. Enter a message; /exit closes this client.\n",safeAgentText(session))
    scanner:=bufio.NewScanner(cmd.InOrStdin());scanner.Buffer(make([]byte,4096),1<<20)
    for scanner.Scan(){
        text:=scanner.Text();if text=="/exit"{return nil};if strings.TrimSpace(text)==""{continue}
        mu.Lock();streamed.Reset();mu.Unlock()
        task,stop:=context.WithTimeout(cmd.Context(),10*time.Minute);reply,err:=client.Send(task,text);stop();if err!=nil{return err}
        mu.Lock();seen:=streamed.String();if seen==""{fmt.Fprint(cmd.OutOrStdout(),safeAgentText(reply))}else if strings.HasPrefix(reply,seen){fmt.Fprint(cmd.OutOrStdout(),safeAgentText(strings.TrimPrefix(reply,seen)))}else if reply!=seen{fmt.Fprint(cmd.OutOrStdout(),"\n",safeAgentText(reply))};fmt.Fprintln(cmd.OutOrStdout());mu.Unlock()
    }
    return scanner.Err()
}
func spawnAgent(cmd *cobra.Command,s store.Store,r janus.SSHRuntime,args []string)error{
    flags:=pflag.NewFlagSet("spawn agent",pflag.ContinueOnError);flags.SetOutput(cmd.ErrOrStderr())
    instance:=flags.String("instance","","Instance name; omit to discover accessible instances")
    kind:=flags.String("type","","Agent implementation or deployment-named ACP identifier")
    port:=flags.IntP("port","p",7263,"SSH port");mode:=flags.String("agent-transport","subsystem","subsystem (default) or stdio");share:=flags.String("share-mcp","","Explicit local MCP allowlist JSON");help:=flags.BoolP("help","h",false,"Show agent help")
    if err:=flags.Parse(args);err!=nil{return err}
    if *help{fmt.Fprintln(cmd.OutOrStdout(),"Usage: janus spawn agent [USER@HOST|TERMINAL_ID|NAME] [-p 7263] [--agent-transport subsystem|stdio] [--instance NAME] [--type NAME] [--share-mcp FILE]\nAgent transport defaults to subsystem for new devices; saved devices retain their transport. Pass a terminal device ID or name, or omit the target to select a saved SSH terminal and reuse its account, host and port. Omit --instance to discover accessible instances (one connects directly; multiple open a selector). Explicit --instance never falls back.");return nil}
    if flags.NArg()>1{return errors.New("use janus spawn agent [USER@HOST|TERMINAL_ID|NAME] [-p PORT]")}
    if *instance!=""&&!janus.ValidAgentType(*instance){return errors.New("instance must match [a-z][a-z0-9-]{0,31}")}
    if flags.Changed("instance")&&*instance==""{return errors.New("--instance requires a nonempty name")}
    if *kind!=""&&!janus.ValidAgentType(*kind){return errors.New("invalid agent type")}
    if *mode!="subsystem"&&*mode!="stdio"{return errors.New("invalid agent transport")}
    selectedMode:=*mode;if !flags.Changed("agent-transport"){selectedMode=""}
    user,host:="","";selectedPort:=*port
    rows,listErr:=s.List();if listErr!=nil{return listErr}
    savedTarget:=flags.NArg()==0||!strings.Contains(flags.Arg(0),"@")
    if !savedTarget{for _,row:=range rows{if row.DevicePlatform()=="terminal"&&row.DeviceType()=="ssh"&&strings.EqualFold(row.Name,flags.Arg(0)){savedTarget=true;break}}}
    if savedTarget{
        var source janus.Profile;var pickErr error
        if flags.NArg()==1{source,pickErr=terminalByReference(rows,flags.Arg(0))}else{source,pickErr=chooseAgentTerminal(rows,func(candidates []selector.Candidate)(selector.Candidate,bool,error){
            return selector.SelectOne(cmd.Context(),os.Stdin,cmd.ErrOrStderr(),selector.Prompt{Title:"Select SSH terminal for agent",EmptySelectionNotice:"Select one terminal before confirming."},candidates)
        })};if pickErr!=nil{return pickErr}
        user,host=source.User,source.Host;if !flags.Changed("port"){selectedPort=source.Port}
    }else{
        var ok bool;user,host,ok=strings.Cut(flags.Arg(0),"@");if !ok{return errors.New("agent destination requires USER@HOST")}
        host=strings.TrimSuffix(strings.TrimPrefix(host,"["),"]")
    }
    selected:=janus.AgentInstance{Name:*instance}
    if selected.Name==""{
        discover,ok:=r.(interface{DiscoverAgentInstances(context.Context,janus.Profile)([]janus.AgentInstance,error)});if !ok{return errors.New("runtime does not support instance discovery; update Janus or specify --instance NAME")}
        probe,err:=s.PrepareAgentDiscovery(user,host,selectedPort,*mode);if err!=nil{return err}
        instances,err:=discover.DiscoverAgentInstances(cmd.Context(),probe);if err!=nil{return err}
        selected,err=chooseAgentInstance(instances,func(candidates []selector.Candidate)(selector.Candidate,bool,error){
            return selector.SelectOne(cmd.Context(),os.Stdin,cmd.ErrOrStderr(),selector.Prompt{Title:"Select agent instance",EmptySelectionNotice:"Select one instance before confirming."},candidates)
        });if err!=nil{return err}
    }
    p,pending,err:=s.PrepareAgentInstance(user,host,selectedPort,selectedMode,*kind,selected.Name);if err!=nil{return err}
    if selected.InstanceID!=""{
        if p.InstanceID!=""&&p.InstanceID!=selected.InstanceID{return errors.New("discovered instance differs from saved binding; saved device was not changed")}
    }
    if pending&&selected.Type!=""{p.Type=selected.Type}

    return connectAgentFlow(cmd,s,r,p,pending,*share,pending,*kind,selected.InstanceID)
}

func terminalByReference(rows []janus.Profile,id string)(janus.Profile,error){
    validate:=func(p janus.Profile)(janus.Profile,error){
        if p.DevicePlatform()!="terminal"||p.DeviceType()!="ssh"{return janus.Profile{},errors.New("device ID or name must refer to an SSH terminal (terminal/ssh)")}
        return p,nil
    }
    for _,p:=range rows{if strings.EqualFold(p.ID,id)||strings.EqualFold(displayID(p.ID),id){return validate(p)}}
    var match *janus.Profile
    for _,p:=range rows{if strings.EqualFold(p.Name,id){
        if match!=nil{return janus.Profile{},errors.New("device name is ambiguous; specify a terminal device ID")}
        copy:=p;match=&copy
    }}
    if match!=nil{return validate(*match)}
    return janus.Profile{},errors.New("terminal device ID or name not found; specify a terminal ID, name or USER@HOST")
}
func chooseAgentTerminal(rows []janus.Profile,pick func([]selector.Candidate)(selector.Candidate,bool,error))(janus.Profile,error){
    candidates:=[]selector.Candidate{};terminals:=map[string]janus.Profile{}
    for _,p:=range rows{if p.DevicePlatform()=="terminal"&&p.DeviceType()=="ssh"{id:=displayID(p.ID);terminals[id]=p;candidates=append(candidates,selector.Candidate{Name:p.Name,Path:id,Detail:p.DeviceType()+" · "+store.Target(p)})}}
    if len(candidates)==0{return janus.Profile{},errors.New("no SSH terminal devices; specify USER@HOST or add one with janus spawn USER@HOST")}
    selected,ok,err:=pick(candidates);if err!=nil{return janus.Profile{},fmt.Errorf("terminal selector: %w; specify USER@HOST when non-interactive",err)}
    if !ok{return janus.Profile{},errors.New("terminal selection cancelled")}
    p,ok:=terminals[selected.Path];if !ok{return janus.Profile{},errors.New("unknown selected terminal")};return p,nil
}
// Each device kind owns its connection flow; selection is independent of it.
type deviceConnectionAdapter struct {
    platform,kind string
    connect func(*cobra.Command,janus.Profile)error
}
func connectDevice(cmd *cobra.Command,p janus.Profile,adapters []deviceConnectionAdapter)error{
    for _,adapter:=range adapters {if adapter.platform==p.DevicePlatform()&&(adapter.kind==p.DeviceType() || adapter.platform=="a2x"&&adapter.kind==""&&p.IsAgent()){return adapter.connect(cmd,p)}}
    return fmt.Errorf("no connection adapter for %s/%s",p.DevicePlatform(),p.DeviceType())
}

func completeShareFile(cmd *cobra.Command,args []string,prefix string)([]string,cobra.ShellCompDirective){
    paths,_:=filepath.Glob(prefix+"*");out:=[]string{}
    for _,path:=range paths {info,err:=os.Stat(path);if err!=nil{continue};if info.IsDir(){path+="/"};out=append(out,path)}
    return out,cobra.ShellCompDirectiveNoFileComp
}
func completeSpawn(cmd *cobra.Command,args []string,prefix string)([]string,cobra.ShellCompDirective){
    if len(args)==0{out:=[]string{};for _,kind:=range []string{"agent","mysql"}{if strings.HasPrefix(kind,prefix){out=append(out,kind)}};return out,cobra.ShellCompDirectiveNoFileComp}
    if len(args)>0&&args[0]=="mysql"{if args[len(args)-1]=="--ca"{return nil,cobra.ShellCompDirectiveDefault};if strings.HasPrefix(prefix,"-"){return []string{"-p","--port","--database","--ca","--help"},cobra.ShellCompDirectiveNoFileComp}}
    if len(args)>0 && args[0]=="agent" {
        if args[len(args)-1]=="--share-mcp"{return completeShareFile(cmd,args,prefix)}
        if args[len(args)-1]=="--agent-transport"{return []string{"subsystem","stdio"},cobra.ShellCompDirectiveNoFileComp}
        if strings.HasPrefix(prefix,"-"){return []string{"-p","--port","--agent-transport","--instance","--type","--share-mcp","--help"},cobra.ShellCompDirectiveNoFileComp}
    }
    return nil,cobra.ShellCompDirectiveNoFileComp
}

func agentSessionTitle(session a2x.Session)string{
    for _,value:=range []string{session.Title,session.Name}{
        title:=strings.Join(strings.Fields(safeAgentText(value))," ")
        if title!=""&&title!=session.ID{return title}
    }
    return "Untitled session"
}

func currentLastSession(sessions []a2x.Session,last string)string{
    for _,session:=range sessions{if session.ID==last{return last}};return ""
}

func chooseAgentSession(sessions []a2x.Session,last string,pick func([]selector.Candidate)(selector.Candidate,bool,error))(string,error){
    seen:=map[string]bool{}
    for _,session:=range sessions {
        if session.ID=="" || len(session.ID)>1024 || safeAgentText(session.ID)!=session.ID || strings.ContainsAny(session.ID,"\n\t") || seen[session.ID] {return "",errors.New("invalid server session ID")}
        seen[session.ID]=true
    }
    if len(sessions)==0{return "",nil}
    if len(sessions)==1{return sessions[0].ID,nil}
    last=currentLastSession(sessions,last)
    candidates:=[]selector.Candidate{}
    // The picker initially focuses the first row; recency changes focus only.
    for _,session:=range sessions{if session.ID==last{candidates=append(candidates,selector.Candidate{Name:agentSessionTitle(session),Path:session.ID})}}
    candidates=append(candidates,selector.Candidate{Name:"New session",Path:""})
    for _,session:=range sessions{if session.ID!=last{candidates=append(candidates,selector.Candidate{Name:agentSessionTitle(session),Path:session.ID})}}
    selected,ok,err:=pick(candidates)
    if err!=nil{return "",fmt.Errorf("session selector: %w; use --empty to create a session",err)}
    if !ok{return "",errors.New("session selection cancelled")}
    if selected.Path!=""&&!seen[selected.Path]{return "",errors.New("unknown selected session")}
    return selected.Path,nil
}

func authenticateAgent(ctx context.Context,client *a2x.Client,r janus.SSHRuntime,p janus.Profile,instanceName string)error{
    setup,cancel:=context.WithTimeout(ctx,30*time.Second);defer cancel()
    if err:=client.Initialize(setup);err!=nil{return err}
    if client.Capabilities().MFARequired {
        prompt,ok:=r.(interface{AgentMFA(context.Context,janus.AgentMFATarget)([]byte,error)});if !ok{return errors.New("runtime does not support native MFA entry")}
        kind:=client.Capabilities().Type
        if !janus.ValidAgentType(kind){kind=""}
        if kind==""&&p.InstanceID!=""&&p.InstanceName==instanceName{kind=p.DeviceType();if !janus.ValidAgentType(kind){kind=""}}
        target:=janus.AgentMFATarget{Target:store.Target(p),Type:kind,InstanceName:instanceName}
        code,err:=prompt.AgentMFA(ctx,target);if err!=nil{return err}
        verification,stop:=context.WithTimeout(ctx,30*time.Second)
        err=client.Verify(verification,string(code));for i:=range code{code[i]=0};stop()
        if err!=nil{return err}
    }
    return nil
}

func verifyConnectedAgent(ctx context.Context,s store.Store,r janus.SSHRuntime,p janus.Profile,client *a2x.Client,pending bool,expected string,expectedInstance ...string)(janus.Profile,deviceprofile.AgentInfo,bool,error){
    var info deviceprofile.AgentInfo
    if err:=authenticateAgent(ctx,client,r,p,p.InstanceName);err!=nil{return p,info,false,err}
    lookup,stop:=context.WithTimeout(ctx,30*time.Second)
    err:=client.Call(lookup,"agent.info",struct{}{},&info);stop()
    legacy:=err!=nil&&a2x.SafeErrorCode(err)==-32601&&!pending&&p.InstanceID==""&&p.DeviceType()=="pi"
    if err!=nil&&!legacy{return p,info,false,err}
    if len(expectedInstance)>0&&expectedInstance[0]!=""&&info.InstanceID!=expectedInstance[0]{return p,info,false,errors.New("authenticated identity differs from discovered instance")}
    if !legacy&&info.InstanceName!=""{name:=p.InstanceName;if name==""{name="default"};if info.InstanceName!=name{return p,info,false,errors.New("authenticated instance name does not match selected route")}}
    if !legacy{p,err=s.RegisterAuthenticatedAgent(p,pending,info.InstanceID,info.Type,expected);if err!=nil{return p,info,false,err}}
    if !legacy{_ = deviceprofile.Agent(s,p,info,client.Capabilities().Terminal)}
    return p,info,legacy,nil
}

func chooseAgentInstance(instances []janus.AgentInstance,pick func([]selector.Candidate)(selector.Candidate,bool,error))(janus.AgentInstance,error){
    if len(instances)==0{return janus.AgentInstance{},errors.New("no accessible agent instances for this SSH user")}
    if len(instances)>128{return janus.AgentInstance{},errors.New("too many discovered instances")}
    candidates:=[]selector.Candidate{};seen:=map[string]janus.AgentInstance{}
    for _,instance:=range instances{
        if _,exists:=seen[instance.Name];exists||!janus.ValidAgentType(instance.Name)||!janus.ValidAgentType(instance.Type)||(instance.Status!="running"&&instance.Status!="stopped"){return janus.AgentInstance{},errors.New("invalid or duplicate discovered instance")}
        seen[instance.Name]=instance;candidates=append(candidates,selector.Candidate{Name:instance.Name,Path:instance.Name,Detail:instance.Type+" · "+instance.Status})
    }
    if len(instances)==1{return instances[0],nil}
    selected,ok,err:=pick(candidates);if err!=nil{return janus.AgentInstance{},fmt.Errorf("instance selector: %w; specify --instance NAME when non-interactive",err)}
    if !ok{return janus.AgentInstance{},errors.New("instance selection cancelled")}
    instance,ok:=seen[selected.Path];if !ok{return janus.AgentInstance{},errors.New("unknown selected instance")};return instance,nil
}
