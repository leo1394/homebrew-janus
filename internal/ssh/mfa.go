package ssh

import (
    "context"
    "errors"
    "os"
    "os/exec"
    "github.com/leo1394/homebrew-janus/pkg/janus"
    "strings"
    "time"
)
func (r *Runtime) AgentMFA(ctx context.Context,target janus.AgentMFATarget)([]byte,error){
    if target.Type!=""&&!janus.ValidAgentType(target.Type){return nil,errors.New("invalid MFA agent type")}
    if target.InstanceName!=""{if err:=ValidateAgentInstance(target.InstanceName);err!=nil{return nil,err}}
    helper,err:=r.helper();if err!=nil{return nil,err}
    bounded,cancel:=context.WithTimeout(ctx,3*time.Minute+5*time.Second);defer cancel()
    command:=exec.CommandContext(bounded,helper,"--agenux-mfa")
    command.Env=append(os.Environ(),"JANUS_MFA_TARGET="+target.Target,"JANUS_MFA_TYPE="+target.Type,"JANUS_MFA_INSTANCE="+target.InstanceName)
    // Never include native stdout/stderr (which could contain a code) in errors.
    code,err:=command.Output()
    if err!=nil {for i:=range code{code[i]=0};return nil,errors.New("MFA entry cancelled or unavailable")}
    if len(code)==0||len(code)>256||strings.ContainsAny(string(code),"\x00\r\n"){for i:=range code{code[i]=0};return nil,errors.New("invalid MFA entry")}
    return code,nil
}
