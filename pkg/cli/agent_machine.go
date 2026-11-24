package cli

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
    "github.com/spf13/cobra"
)

// Machine calls use exactly the interactive transport and native MFA gate.
// They do not select/open Pi sessions, persist contexts, or discover local tools.
func agentMachineCommand(s store.Store,r janus.SSHRuntime)*cobra.Command{
    root:=&cobra.Command{Use:"agent",Short:"Call a saved agent with JSON output and native authentication."}
    for _,operation:=range []string{"info","status","send"}{
        operation:=operation
        var contextID string
        use:=operation+" DEVICE";count:=1;if operation=="send"{use+=" TEXT";count=2}
        command:=&cobra.Command{Use:use,Short:"Call agent "+operation+"; print one JSON result.",RunE:func(cmd *cobra.Command,args []string)error{
            result,err:=func()(json.RawMessage,error){
                if len(args)!=count{return nil,fmt.Errorf("expected janus agent %s",use)}
                if operation=="send"&&strings.TrimSpace(args[1])==""{return nil,errors.New("message text must not be empty")}
                rows,err:=s.List();if err!=nil{return nil,err};p,err:=resolve(rows,args[0]);if err!=nil{return nil,err}
                if !p.IsAgent(){return nil,errors.New("device must be an a2x agent")}
                transport,ok:=r.(agentTransport);if !ok{return nil,errors.New("runtime does not support Agenux")}
                stream,err:=transport.Agenux(cmd.Context(),p);if err!=nil{return nil,err};defer stream.Close()
                client,err:=a2x.New(stream,nil,nil);if err!=nil{return nil,err};defer client.Close()
                if err:=authenticateAgent(cmd.Context(),client,r,p,p.InstanceName);err!=nil{return nil,err}
                if p.InstanceName!=""{
                    request,stop:=context.WithTimeout(cmd.Context(),30*time.Second)
                    var info struct{InstanceID string `json:"instanceId"`;Type string `json:"type"`};err:=client.Call(request,"agent.info",struct{}{},&info);stop();if err!=nil{return nil,err}
                    if info.InstanceID!=p.InstanceID||info.Type!=p.DeviceType(){return nil,errors.New("named device identity changed; owner review required")}
                }
                method:="agent."+operation;params:=any(struct{}{})
                if operation=="send"{
                    id:=make([]byte,16);if _,err:=rand.Read(id);err!=nil{return nil,err}
                    message:=map[string]any{"kind":"message","role":"user","messageId":hex.EncodeToString(id),"parts":[]map[string]string{{"kind":"text","text":args[1]}}}
                    if contextID!=""{message["contextId"]=contextID}
                    method="message/send";params=map[string]any{"message":message}
                }
                timeout:=30*time.Second;if operation=="send"{timeout=10*time.Minute}
                request,cancel:=context.WithTimeout(cmd.Context(),timeout);defer cancel()
                var result json.RawMessage
                if err:=client.Call(request,method,params,&result);err!=nil{return nil,err}
                return result,nil
            }()
            response:=map[string]any{"ok":err==nil}
            if err!=nil{response["error"]=err.Error()}else{response["result"]=result}
            if writeErr:=json.NewEncoder(cmd.OutOrStdout()).Encode(response);writeErr!=nil{return errors.Join(err,writeErr)}
            return err
        }}
        if operation=="send"{command.Flags().StringVar(&contextID,"context-id","","Continue an explicit A2A context (never persisted locally)")}
        root.AddCommand(command)
    }
    addAgentServiceCommands(root,s,r)
    return root
}
