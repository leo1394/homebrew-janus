package cli

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os/signal"
    "os"
    "syscall"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/leo1394/homebrew-janus/internal/agentservice"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/internal/ssh"
    "github.com/leo1394/homebrew-janus/pkg/janus"
    "github.com/spf13/cobra"
)
func serviceJSON(cmd *cobra.Command,response agentservice.Response,err error)error{
    if err!=nil{response=agentservice.Response{Error:err.Error()}}
    if writeErr:=json.NewEncoder(cmd.OutOrStdout()).Encode(response);writeErr!=nil{return errors.Join(err,writeErr)}
    if !response.OK{return errors.New(response.Error)};return nil
}
func addAgentServiceCommands(root *cobra.Command,s store.Store,r janus.SSHRuntime){
    var directory,instanceName,delegateDirectory string
    var registerInstance bool
    var delegateUIDs []int
    serve:=&cobra.Command{Use:"service DEVICE",Short:"Keep one owner-authenticated agent connection alive (foreground).",Args:cobra.ExactArgs(1),RunE:func(cmd *cobra.Command,args []string)error{
        if registerInstance&&instanceName==""{return errors.New("--register-instance requires --instance")}
        if directory==""{return errors.New("--directory is required")}
        options:=agentservice.ServeOptions{DelegateDirectory:delegateDirectory}
        for _,uid:=range delegateUIDs{if uid<=0||uint64(uid)>4294967295{return errors.New("invalid delegate UID")};options.DelegateUIDs=append(options.DelegateUIDs,uint32(uid))}
        rows,err:=s.List();if err!=nil{return err};p,err:=resolve(rows,args[0]);if err!=nil{return err};if !p.IsAgent(){return errors.New("device is not an a2x agent")}
        if instanceName!=""{if err:=ssh.ValidateAgentInstance(instanceName);err!=nil{return err}}
        ctx,cancel:=signal.NotifyContext(cmd.Context(),syscall.SIGINT,syscall.SIGTERM,syscall.SIGHUP);defer cancel()
        transport,ok:=r.(agentTransport);if !ok{return errors.New("runtime does not support Agenux")}
        var stream io.ReadWriteCloser
        if instanceName==""{stream,err=transport.Agenux(ctx,p)}else{
            named,ok:=r.(interface{AgenuxInstance(context.Context,janus.Profile,string)(io.ReadWriteCloser,error)});if !ok{return errors.New("runtime does not support named instances")}
            stream,err=named.AgenuxInstance(ctx,p,instanceName)
        }
        if err!=nil{return err};defer stream.Close()
        client,err:=a2x.New(stream,nil,nil);if err!=nil{return err};defer client.Close()
        displayInstance:=instanceName;if displayInstance==""{displayInstance=p.InstanceName}
        if err:=authenticateAgent(ctx,client,r,p,displayInstance);err!=nil{return err}
        request,stop:=context.WithTimeout(ctx,30*time.Second)
        var info struct{InstanceID string `json:"instanceId"`;InstanceName string `json:"instanceName"`;Type string `json:"type"`};err=client.Call(request,"agent.info",struct{}{},&info);stop();if err!=nil{return err}
        if info.InstanceID==""{return errors.New("agent.info did not provide instanceId")}
        route:=displayInstance;if route==""{route="default"};if info.InstanceName!=""&&info.InstanceName!=route{return errors.New("authenticated instance name does not match selected route")}
        registeredDeviceID:=""
        if registerInstance{saved,err:=s.RegisterNamedAgent(p,instanceName,info.InstanceID,info.Type);if err!=nil{return err};registeredDeviceID=saved.ID}else if instanceName==""{
            if p.InstanceName!=""&&p.DeviceType()!=info.Type{return errors.New("named device type changed; owner review required")}
            if err:=s.BindAgentIdentity(p.ID,info.InstanceID,info.Type);err!=nil{return err}
        }
        effectiveInstance:=instanceName;if effectiveInstance==""{effectiveInstance=p.InstanceName}
        return agentservice.ServeIsolated(ctx,directory,info.InstanceID,client,options,func(){
            json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"ready":true,"directory":directory,"instanceId":info.InstanceID,"instance":effectiveInstance,"registeredDeviceId":registeredDeviceID,"delegateDirectory":delegateDirectory,"ownerUid":os.Getuid()})
        })
    }}
    serve.Flags().StringVar(&directory,"directory","","Private absolute directory for owner and delegated sockets")
    serve.Flags().BoolVar(&registerInstance,"register-instance",false,"Register the authenticated named instance as a separate device")
    serve.Flags().StringVar(&instanceName,"instance","","Transient named instance; does not change the default device binding")
    serve.Flags().StringVar(&delegateDirectory,"delegate-directory","","Separate traversable directory containing only delegated IPC")
    serve.Flags().IntSliceVar(&delegateUIDs,"delegate-uid",nil,"Allowed distinct non-root OS UIDs for delegated IPC")
    root.AddCommand(serve)
    grant:=&cobra.Command{Use:"grant",Short:"Owner grant management through the connection service."}
    var grantDir,actor,output,profile string;var ttl,concurrent int
    create:=&cobra.Command{Use:"create",Short:"Create a named-profile grant; write credentials only to a new private file.",Args:cobra.NoArgs,RunE:func(cmd *cobra.Command,args []string)error{
        if output==""{return serviceJSON(cmd,agentservice.Response{},errors.New("--credential-out is required"))}
        params,_:=json.Marshal(map[string]any{"actorId":actor,"profile":profile,"ttlSeconds":ttl,"maxConcurrent":concurrent})
        q:=agentservice.Request{Method:"grant.create",Params:params};if err:=agentservice.Validate(q,true);err!=nil{return serviceJSON(cmd,agentservice.Response{},err)}
        response,err:=agentservice.Call(cmd.Context(),grantDir,true,q)
        if err!=nil||!response.OK{return serviceJSON(cmd,response,err)}
        var credential agentservice.Credential
        if json.Unmarshal(response.Result,&credential)!=nil{return serviceJSON(cmd,agentservice.Response{},errors.New("invalid grant response"))}
        ownerUID:=uint32(os.Getuid());credential.LocalOwnerUID=&ownerUID
        if err:=agentservice.WriteCredential(output,credential);err!=nil{
            // Failed local delivery must not intentionally leave a usable grant.
            revoke,_:=json.Marshal(map[string]string{"grantId":credential.GrantID})
            cleanup,stop:=context.WithTimeout(context.Background(),5*time.Second);defer stop()
            revoked,revokeErr:=agentservice.Call(cleanup,grantDir,true,agentservice.Request{Method:"grant.revoke",Params:revoke})
            if revokeErr!=nil||!revoked.OK{return serviceJSON(cmd,agentservice.Response{},fmt.Errorf("credential file not written; revoke grant %s manually",credential.GrantID))}
            return serviceJSON(cmd,agentservice.Response{},errors.New("credential file not written; grant revoked"))
        }
        result,_:=json.Marshal(map[string]string{"grantId":credential.GrantID,"instanceId":credential.InstanceID,"credentialFile":output})
        return serviceJSON(cmd,agentservice.Response{OK:true,Result:result},nil)
    }}
    create.Flags().StringVar(&grantDir,"directory","","Connection service directory");create.Flags().StringVar(&actor,"actor-id","","Delegated actor identity");create.Flags().StringVar(&output,"credential-out","","New credential file in an existing private directory")
    create.Flags().StringVar(&profile,"profile","pi-report","Administrator-registered profile name (never a command or path)")
    create.Flags().IntVar(&ttl,"ttl-seconds",3600,"Grant lifetime (1..86400 seconds)");create.Flags().IntVar(&concurrent,"max-concurrent",1,"Concurrent tasks (1..8)")
    var revokeDir string
    revoke:=&cobra.Command{Use:"revoke GRANT_ID",Short:"Revoke a grant as owner.",Args:cobra.ExactArgs(1),RunE:func(cmd *cobra.Command,args []string)error{
        params,_:=json.Marshal(map[string]string{"grantId":args[0]});response,err:=agentservice.Call(cmd.Context(),revokeDir,true,agentservice.Request{Method:"grant.revoke",Params:params});return serviceJSON(cmd,response,err)
    }};revoke.Flags().StringVar(&revokeDir,"directory","","Connection service directory");grant.AddCommand(create,revoke);root.AddCommand(grant)
    tasks:=&cobra.Command{Use:"tasks",Short:"Owner task queries through the connection service."}
    for _,method:=range []string{"list","get"}{
        method:=method;var dir string;use:=method;count:=0;if method=="get"{use+=" TASK_ID";count=1}
        command:=&cobra.Command{Use:use,Args:cobra.ExactArgs(count),RunE:func(cmd *cobra.Command,args []string)error{
            params:=map[string]string{};if method=="get"{params["taskId"]=args[0]};data,_:=json.Marshal(params)
            response,err:=agentservice.Call(cmd.Context(),dir,true,agentservice.Request{Method:"tasks."+method,Params:data});return serviceJSON(cmd,response,err)
        }};command.Flags().StringVar(&dir,"directory","","Connection service directory");tasks.AddCommand(command)
    };root.AddCommand(tasks)
    var delegatedDir,credentialFile,method string
    delegated:=&cobra.Command{Use:"delegated",Short:"Call an allowed method with private grant credentials; read JSON params from stdin.",Args:cobra.NoArgs,RunE:func(cmd *cobra.Command,args []string)error{
        credential,err:=agentservice.ReadCredential(credentialFile);if err!=nil{return serviceJSON(cmd,agentservice.Response{},err)}
        params,err:=io.ReadAll(io.LimitReader(cmd.InOrStdin(),(1<<20)+1));if err!=nil||len(params)>1<<20{return serviceJSON(cmd,agentservice.Response{},errors.New("invalid or oversized params"))}
        q:=agentservice.Request{InstanceID:credential.InstanceID,GrantID:credential.GrantID,Credential:credential.Credential,Method:method,Params:params}
        if err:=agentservice.Validate(q,false);err!=nil{return serviceJSON(cmd,agentservice.Response{},err)}
        response,err:=agentservice.CallCredential(cmd.Context(),delegatedDir,credential,method,params);return serviceJSON(cmd,response,err)
    }}
    delegated.Flags().StringVar(&delegatedDir,"directory","","Connection service directory");delegated.Flags().StringVar(&credentialFile,"credential-file","","Private grant file (never pass the credential itself)");delegated.Flags().StringVar(&method,"method","","tasks.submit, tasks.get, tasks.list, tasks.events or agent.status")
    root.AddCommand(delegated)
    addAgentHTTPCommand(root)
}
