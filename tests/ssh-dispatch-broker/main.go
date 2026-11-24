// Acceptance transport launcher for a dedicated Colima VM's existing SSH master.
package main

import (
    "context"
    "io"
    "log"
    "os"
    "os/exec"
    "regexp"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/leo1394/homebrew-janus/internal/agentservice"
)

type channel struct { io.ReadCloser; io.Writer }

func main(){
    if len(os.Args)!=4 || !regexp.MustCompile(`^dispatch-smoke-[a-f0-9]+$`).MatchString(os.Args[3]){log.Fatal("expected service directory, Colima SSH config and test container")}
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute)
    defer cancel()
    cmd:=exec.CommandContext(ctx,"ssh","-F",os.Args[2],"-o","ControlMaster=no","-o","ProxyCommand=false","-T","lima-colima-agenux-accept","docker","exec","-i",os.Args[3],"bun","/app/agenux.mjs","stdio")
    cmd.Stderr=os.Stderr
    stdin,err:=cmd.StdinPipe();if err!=nil{log.Fatal(err)}
    stdout,err:=cmd.StdoutPipe();if err!=nil{log.Fatal(err)}
    if err=cmd.Start();err!=nil{log.Fatal(err)}
    defer func(){stdin.Close();cmd.Wait()}()
    client,err:=a2x.New(channel{stdout,stdin},nil,nil);if err!=nil{log.Fatal(err)}
    defer client.Close()
    if err=client.Initialize(ctx);err!=nil{log.Fatal(err)}
    var info struct{ InstanceID string `json:"instanceId"` }
    if err=client.Call(ctx,"agent.info",map[string]any{},&info);err!=nil || info.InstanceID==""{log.Fatal("missing agent identity: ",err)}
    if err=agentservice.Serve(ctx,os.Args[1],info.InstanceID,client,nil);err!=nil && ctx.Err()==nil{log.Fatal(err)}
}
