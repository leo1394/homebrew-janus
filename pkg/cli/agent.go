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
    "github.com/leo1394/homebrew-janus/internal/mcpshare"
    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)
type agentTransport interface { Agenux(context.Context,janus.Profile)(io.ReadWriteCloser,error) }
func safeAgentText(value string)string{return strings.Map(func(r rune)rune{if r=='\n'||r=='\t'||r>=32&&r!=127&&!(r>=128&&r<=159){return r};return -1},value)}
func connectAgent(cmd *cobra.Command,s store.Store,r janus.SSHRuntime,p janus.Profile,empty bool,share string)error{return connectAgentFlow(cmd,s,r,p,empty,share,false,"")}
func connectAgentFlow(cmd *cobra.Command,s store.Store,r janus.SSHRuntime,p janus.Profile,empty bool,share string,pending bool,expected string)error{
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
    if err:=authenticateAgent(cmd.Context(),client,r,p,p.InstanceName);err!=nil{return err}
    lookup,stop:=context.WithTimeout(cmd.Context(),30*time.Second)
    var info struct{InstanceID string `json:"instanceId"`;Type string `json:"type"`;Capabilities struct{DelegatedTasks bool `json:"delegatedTasks"`} `json:"capabilities"`}
    err=client.Call(lookup,"agent.info",struct{}{},&info);stop()
    legacy:=err!=nil&&a2x.SafeErrorCode(err)==-32601&&!pending&&p.InstanceID==""&&p.DeviceType()=="pi"
    if err!=nil&&!legacy{return err}
    if !legacy{p,err=s.RegisterAuthenticatedAgent(p,pending,info.InstanceID,info.Type,expected);if err!=nil{return err};registered=true}
    if !client.Capabilities().Terminal&&info.Capabilities.DelegatedTasks{
        fmt.Fprintf(cmd.OutOrStdout(),"Agent %s registered and authenticated (type=%s). This instance supports machine tasks, not an interactive terminal. Use janus agent service %s --directory <PRIVATE_DIRECTORY> to start a task connection.\n",safeAgentText(p.Name),p.DeviceType(),displayID(p.ID));return nil
    }
    if !client.Capabilities().Terminal&&p.DeviceType()!="pi"{return errors.New("agent registered, but no native terminal or delegated task capability was advertised")}
    if isTTY && !client.Capabilities().Terminal{return errors.New("server does not support native Pi terminal UI; upgrade Agenux (non-TTY task.send remains available)")}
    session:=""
    if !empty {
        listing,stop:=context.WithTimeout(cmd.Context(),30*time.Second);sessions,err:=client.Sessions(listing);stop();if err!=nil{return err}
        session,err=chooseAgentSession(sessions,p.LastSession,func(candidates []selector.Candidate)(selector.Candidate,bool,error){
            return selector.SelectOne(cmd.Context(),os.Stdin,cmd.ErrOrStderr(),selector.Prompt{Title:"Select agent session"},candidates)
        })
        if err!=nil{return err}
    }
    // Selection time is not part of the remote request deadline.
    opened,stop:=context.WithTimeout(cmd.Context(),30*time.Second)
    session,err=client.Open(opened,session);stop();if err!=nil{return err}
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
    kind:=flags.String("type","","Agent implementation or deployment-named ACP identifier")
    port:=flags.IntP("port","p",7263,"SSH port");mode:=flags.String("agent-transport","subsystem","subsystem or stdio");share:=flags.String("share-mcp","","Explicit local MCP allowlist JSON");help:=flags.BoolP("help","h",false,"Show agent help")
    if err:=flags.Parse(args);err!=nil{return err}
    if *help{fmt.Fprintln(cmd.OutOrStdout(),"Usage: janus spawn agent USER@HOST [-p 7263] [--agent-transport subsystem|stdio] [--type NAME] [--share-mcp FILE]");return nil}
    if flags.NArg()!=1{return errors.New("use janus spawn agent USER@HOST [-p 7263]")}
    user,host,ok:=strings.Cut(flags.Arg(0),"@");if !ok{return errors.New("agent destination requires USER@HOST")}
    host=strings.TrimSuffix(strings.TrimPrefix(host,"["),"]")
    selectedMode:=*mode;if !flags.Changed("agent-transport"){selectedMode=""}
    p,pending,err:=s.PrepareAgent(user,host,*port,selectedMode,*kind);if err!=nil{return err}
    return connectAgentFlow(cmd,s,r,p,true,*share,pending,*kind)
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
    if len(args)==0 && strings.HasPrefix("agent",prefix){return []string{"agent"},cobra.ShellCompDirectiveNoFileComp}
    if len(args)>0 && args[0]=="agent" {
        if args[len(args)-1]=="--share-mcp"{return completeShareFile(cmd,args,prefix)}
        if args[len(args)-1]=="--agent-transport"{return []string{"subsystem","stdio"},cobra.ShellCompDirectiveNoFileComp}
        if strings.HasPrefix(prefix,"-"){return []string{"-p","--port","--agent-transport","--type","--share-mcp","--help"},cobra.ShellCompDirectiveNoFileComp}
    }
    return nil,cobra.ShellCompDirectiveNoFileComp
}

func chooseAgentSession(sessions []a2x.Session,last string,pick func([]selector.Candidate)(selector.Candidate,bool,error))(string,error){
    seen:=map[string]bool{}
    for _,session:=range sessions {
        if session.ID=="" || len(session.ID)>1024 || safeAgentText(session.ID)!=session.ID || strings.ContainsAny(session.ID,"\n\t") || seen[session.ID] {return "",errors.New("invalid server session ID")}
        seen[session.ID]=true
    }
    if len(sessions)==0{return "",nil}
    if len(sessions)==1{return sessions[0].ID,nil}
    candidates:=[]selector.Candidate{}
    // The picker initially focuses the first row; recency changes focus only.
    for _,session:=range sessions{if session.ID==last{candidates=append(candidates,selector.Candidate{Name:safeAgentText(session.Name),Path:session.ID})}}
    candidates=append(candidates,selector.Candidate{Name:"New session",Path:""})
    for _,session:=range sessions{if session.ID!=last{candidates=append(candidates,selector.Candidate{Name:safeAgentText(session.Name),Path:session.ID})}}
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
