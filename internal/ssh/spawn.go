package ssh

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "os/exec"
    "strconv"
    "strings"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)

// normalize preserves SSH option values and the remote command. It also accepts
// the historical Janus spelling USER@HOST -p PORT, which OpenSSH treats as a command.
func normalize(args []string) []string {
    out:=append([]string(nil),args...)
    if len(out)>=3 && !strings.HasPrefix(out[0],"-") && out[1]=="-p" {
        out=append([]string{"-p",out[2],out[0]},out[3:]...)
    }
    return out
}
func resolvedProfile(data string)(janus.Profile,error){
    p:=janus.Profile{}
    scan:=bufio.NewScanner(strings.NewReader(data))
    for scan.Scan(){key,value,ok:=strings.Cut(scan.Text()," ");if !ok{continue};switch key{case "hostname":p.Host=value;case "user":p.User=value;case "port":p.Port,_=strconv.Atoi(value)}}
    if err:=scan.Err();err!=nil{return p,err}
    if p.Host==""||p.User==""||p.Port<1||p.Port>65535{return p,errors.New("SSH did not resolve a valid user, host and port")}
    return p,nil
}
func (r *Runtime) Spawn(ctx context.Context,args []string)error{
    args=normalize(args)
    query:=exec.CommandContext(ctx,"/usr/bin/ssh",append([]string{"-G","-T"},args...)...)
    query.Stderr=r.Err
    data,err:=query.Output();if err!=nil{return fmt.Errorf("invalid SSH arguments: %w",err)}
    target,err:=resolvedProfile(string(data));if err!=nil{return err}
    options,err:=transportOptions(args);if err!=nil{return err}
    p,_,err:=r.Store.Add(target.User,target.Host,target.Port);if err!=nil{return err}
    if err:=r.ensure(ctx,p,options);err!=nil{return err}
    r.startProfile(p)
    // Janus owns transport/authentication; caller arguments control the attached
    // session. First-value SSH options prevent overriding the master boundary.
    prefix:=[]string{"-S",r.path(p),"-o","ControlMaster=no","-o","BatchMode=yes","-o","ProxyCommand=/usr/bin/false","-o","HostName="+p.Host,"-l",p.User,"-p",strconv.Itoa(p.Port)}
    return r.run(ctx,append(prefix,args...),nil,true)
}

// Separate pre-destination options from the remote command without shell parsing.
func transportOptions(args []string)([]string,error){
    options:=[]string{}
    values:="BbcDEeFIiJLlmOoPpQRSWw"
    for i:=0;i<len(args);i++{
        arg:=args[i]
        if arg=="--"{break}
        if !strings.HasPrefix(arg,"-")||arg=="-"{break}
        if arg=="-O"||strings.HasPrefix(arg,"-O")||arg=="-M"||arg=="-S"||strings.HasPrefix(arg,"-S"){return nil,errors.New("Janus Confidential CLI owns the SSH master; use connect/disconnect instead of -M, -S or -O")}
        options=append(options,arg)
        for j:=1;j<len(arg);j++{
            if strings.ContainsRune(values,rune(arg[j])){
                if j==len(arg)-1{i++;if i>=len(args){return nil,errors.New("SSH option requires a value")};options=append(options,args[i])}
                break
            }
        }
    }
    return options,nil
}
