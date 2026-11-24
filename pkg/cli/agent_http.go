package cli

import (
    "context"
    "encoding/json"
    "errors"
    "os/signal"
    "syscall"

    "github.com/leo1394/homebrew-janus/internal/a2ahttp"
    "github.com/leo1394/homebrew-janus/internal/agentservice"
    "github.com/spf13/cobra"
)
func addAgentHTTPCommand(root *cobra.Command){
    var directory,file,address string
    command:=&cobra.Command{Use:"a2a",Short:"Expose one delegated grant as local A2A 1.0 JSON-RPC/SSE.",Args:cobra.NoArgs,RunE:func(cmd *cobra.Command,args []string)error{
        credential,err:=agentservice.ReadCredential(file);if err!=nil{return err}
        ctx,cancel:=signal.NotifyContext(cmd.Context(),syscall.SIGINT,syscall.SIGTERM,syscall.SIGHUP);defer cancel()
        call:=func(ctx context.Context,method string,params json.RawMessage)(agentservice.Response,error){
            return agentservice.CallCredential(ctx,directory,credential,method,params)
        }
        probe,err:=call(ctx,"agent.card",json.RawMessage(`{}`));if err!=nil{return err};if !probe.OK{return errors.New("grant is not authorized for an Agent Card")}
        adapter:=a2ahttp.New(credential.Credential,call)
        return a2ahttp.Serve(ctx,address,adapter,func(url string){json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"ready":true,"url":url+"/rpc","agentCard":url+"/.well-known/agent-card.json","instanceId":credential.InstanceID})})
    }}
    command.Flags().StringVar(&directory,"directory","","M1 connection service directory")
    command.Flags().StringVar(&file,"credential-file","","Private grant file; its credential is the local HTTP Bearer token")
    command.Flags().StringVar(&address,"listen","127.0.0.1:0","Numeric loopback listen address (port 0 selects a free port)")
    root.AddCommand(command)
}
