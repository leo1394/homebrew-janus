package ssh

import (
    "context"
    "io"
    "errors"
    "os"
    "path/filepath"
    "syscall"
    "os/exec"
    "sync"
    "regexp"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)

type subsystemStream struct {
    io.ReadCloser
    input io.WriteCloser
    command *exec.Cmd
    once sync.Once
    err error
}
func (s *subsystemStream) Write(data []byte)(int,error){return s.input.Write(data)}
func (s *subsystemStream) Close()error{
    s.once.Do(func(){s.input.Close();s.ReadCloser.Close();s.command.Process.Kill();s.err=s.command.Wait()})
    return s.err
}
// This fixed script contains no interpolated profile or caller arguments.
// A non-interactive SSH environment may omit the user's local bin directory.
const agenuxStdioLauncher = `/bin/sh -c 'if agenux_path=$(command -v agenux); then exec "$agenux_path" stdio; else exec "$HOME/.local/bin/agenux" stdio; fi'`

// Agenux authenticates through the existing master, then requests the fixed
// subsystem or stdio launcher. No PTY or independent fallback authentication.
func (r *Runtime) Agenux(ctx context.Context,p janus.Profile)(io.ReadWriteCloser,error){
    args,err:=r.agenuxArguments(ctx,p);if err!=nil{return nil,err}
    return r.startAgenux(ctx,args)
}
func (r *Runtime) startAgenux(ctx context.Context,args []string)(io.ReadWriteCloser,error){
    command:=exec.CommandContext(ctx,"/usr/bin/ssh",args...);command.Stderr=r.Err
    input,err:=command.StdinPipe();if err!=nil{return nil,err}
    output,err:=command.StdoutPipe();if err!=nil{input.Close();return nil,err}
    if err:=command.Start();err!=nil{input.Close();output.Close();return nil,err}
    return &subsystemStream{ReadCloser:output,input:input,command:command},nil
}

func validateAgentSocket(path string)error{
    if !filepath.IsAbs(path){return errors.New("borrowed control socket must be absolute")}
    info,err:=os.Lstat(path);if err!=nil{return err}
    stat,ok:=info.Sys().(*syscall.Stat_t)
    if !ok||info.Mode()&os.ModeSocket==0||info.Mode()&os.ModeSymlink!=0||stat.Uid!=uint32(os.Getuid()){return errors.New("borrowed control socket must be a real socket owned by this user")}
    return nil
}

func (r *Runtime) agenuxArguments(ctx context.Context,p janus.Profile)([]string,error){return r.agenuxRegistrationArguments(ctx,p,false)}
func (r *Runtime) agenuxRegistrationArguments(ctx context.Context,p janus.Profile,pending bool)([]string,error){
    if p.InstanceName!=""{if err:=ValidateAgentInstance(p.InstanceName);err!=nil{return nil,err};if p.AgentTransport!="stdio"{return nil,errors.New("named instances require stdio transport")}}
    base:=r.base(p)
    if r.AgentControlSocket!="" {
        if r.CredentialNamespace=="" {return nil,errors.New("borrowed agent master requires isolated development credentials")}
        if err:=validateAgentSocket(r.AgentControlSocket);err!=nil{return nil,err}
        base=[]string{"-F","/dev/null","-S",r.AgentControlSocket}
    } else if err:=r.ensureRegistration(ctx,p,nil,pending);err!=nil{return nil,err}
    args:=append(base,"-o","ControlMaster=no","-o","BatchMode=yes","-o","ProxyCommand=/usr/bin/false","-T")
    if p.AgentTransport=="stdio" { args=append(args,p.Host,agenuxStdioLauncher) } else { args=append(args,"-s",p.Host,"agenux") }
    if p.InstanceName!=""{args[len(args)-1]=`exec "$HOME/.local/bin/agenux-instance" `+p.InstanceName+` stdio`}
    return args,nil
}

var agentInstancePattern=regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
func ValidateAgentInstance(name string)error{
    if !agentInstancePattern.MatchString(name){return errors.New("instance must match [a-z][a-z0-9-]{0,31}")};return nil
}
func (r *Runtime) agenuxInstanceArguments(ctx context.Context,p janus.Profile,name string)([]string,error){
    if err:=ValidateAgentInstance(name);err!=nil{return nil,err}
    if p.AgentTransport!="stdio"{return nil,errors.New("named instances require stdio transport")}
    args,err:=r.agenuxArguments(ctx,p);if err!=nil{return nil,err}
    args[len(args)-1]=`exec "$HOME/.local/bin/agenux-instance" `+name+` stdio`
    return args,nil
}
func (r *Runtime) AgenuxInstance(ctx context.Context,p janus.Profile,name string)(io.ReadWriteCloser,error){
    args,err:=r.agenuxInstanceArguments(ctx,p,name);if err!=nil{return nil,err};return r.startAgenux(ctx,args)
}

func(r *Runtime)AgenuxPending(ctx context.Context,p janus.Profile)(io.ReadWriteCloser,error){
    args,err:=r.agenuxRegistrationArguments(ctx,p,true);if err!=nil{return nil,err};return r.startAgenux(ctx,args)
}
func(r *Runtime)DiscardPendingAgent(ctx context.Context,p janus.Profile)error{
    if r.AgentControlSocket!=""{return nil};return r.Disconnect(ctx,p)
}
