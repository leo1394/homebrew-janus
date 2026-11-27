package cli

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "sync"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
    "github.com/spf13/cobra"
)

// One process owns one authenticated device. Requests run serially and deltas
// are emitted while task.send is awaiting the authoritative final response.
func bridgeCommand(s store.Store,r janus.SSHRuntime)*cobra.Command{
    return &cobra.Command{Use:"bridge [ID|NAME]",Short:"Bridge desktop clients to authenticated A2X agent sessions.",Long:"Exchange newline-delimited JSON over stdin/stdout for desktop client integration. Without a target, list saved A2X agent devices as JSON. With an ID or name, connect to a verified Pi agent using the existing SSH and native authentication flow.\n\nSupports sessions, projects, history, model and skill discovery, skill installation, and streaming messages, subject to server capabilities. One device is connected per bridge process.",Example:"  janus bridge\n  janus bridge my-agent",Args:cobra.MaximumNArgs(1),RunE:func(cmd *cobra.Command,args []string)error{
        rows,err:=s.List();if err!=nil{return err}
        enc:=json.NewEncoder(cmd.OutOrStdout())
        if len(args)==0{
            devices:=[]map[string]string{}
            for _,p:=range rows{if p.IsAgent(){devices=append(devices,map[string]string{"id":p.ID,"name":p.Name,"type":p.DeviceType()})}}
            return enc.Encode(map[string]any{"version":1,"devices":devices})
        }
        p,err:=resolve(rows,args[0]);if err!=nil{return err}
        transport,ok:=r.(agentTransport);if !ok||!p.IsAgent(){return errors.New("A2X device required")}
        stream,err:=transport.Agenux(cmd.Context(),p);if err!=nil{return err};defer stream.Close()
        var mu sync.Mutex
        emit:=func(value any)error{mu.Lock();defer mu.Unlock();return enc.Encode(value)}
        client,err:=a2x.New(stream,nil,func(d a2x.Delta){emit(map[string]any{"event":"delta","session":d.SessionID,"text":d.Text,"phase":d.Phase})});if err!=nil{return err};defer client.Close()
        p,info,legacy,err:=verifyConnectedAgent(cmd.Context(),s,r,p,client,false,"");if err!=nil{return err}
        if legacy||info.Type!="pi"{return errors.New("此设备未提供可验证的 Pi 交互会话")}
        history:=false
        for _,method:=range info.Methods{if method=="session.history"{history=true}}
        ctx,stop:=context.WithTimeout(cmd.Context(),30*time.Second)
        sessions,err:=client.Sessions(ctx);stop();if err!=nil{return err}
        if err=emit(map[string]any{"event":"ready","sessions":sessions,"last":currentLastSession(sessions,p.LastSession),"methods":info.Methods,"history":history,"instance":info.InstanceID,"principal":info.Principal,"workspaceIsolation":info.Capabilities.WorkspaceIsolation});err!=nil{return err}
        scanner:=bufio.NewScanner(cmd.InOrStdin());scanner.Buffer(make([]byte,4096),1<<20)
        lines:=make(chan []byte)
        finished:=make(chan struct{});defer close(finished)
        go func(){defer close(lines);for scanner.Scan(){line:=append([]byte(nil),scanner.Bytes()...);select{case lines<-line:case <-finished:return}}}()
        for {
            var line []byte
            select{case <-client.Done():return errors.New("远端设备已断开");case data,ok:=<-lines:if !ok{return scanner.Err()};line=data}
            var q struct{ID,Method,Session,Text,Name,Project,Query,Source string;Before *int; Model json.RawMessage; Images json.RawMessage}
            if err=json.Unmarshal(line,&q);err!=nil{return err}
            ctx,stop:=context.WithTimeout(cmd.Context(),10*time.Minute)
            var value any
            switch q.Method{
            case "sessions":
                value,err=client.Sessions(ctx)
            case "open":
                var id string
                if q.Project!=""{
                    var opened struct{SessionID string `json:"sessionId"`}
                    err=client.Call(ctx,"session.open",map[string]string{"project":q.Project},&opened);id=opened.SessionID
                }else{id,err=client.Open(ctx,q.Session)}
                value=id
                if err==nil{err=s.SetSession(p.ID,id)}
            case "project.list","project.create","session.rename","session.delete":
                var result json.RawMessage
                err=client.Call(ctx,q.Method,map[string]string{"sessionId":q.Session,"name":q.Name},&result);value=result
            case "skill.search","skill.install":
                params:=map[string]string{"query":q.Query}
                if q.Method=="skill.install"{params=map[string]string{"name":q.Name,"source":q.Source}}
                var result json.RawMessage
                err=client.Call(ctx,q.Method,params,&result);value=result
            case "catalog":
                var catalog json.RawMessage
                err=client.Call(ctx,"composer.catalog",struct{}{},&catalog);value=catalog
            case "history":
                var page json.RawMessage
                err=client.Call(ctx,"session.history",map[string]any{"sessionId":q.Session,"before":q.Before},&page);value=page
            case "send":
                _,err=client.Open(ctx,q.Session)
                if err==nil{
                    params:=map[string]any{"sessionId":q.Session,"text":q.Text}
                    if len(q.Model)>0{params["model"]=q.Model}
                    if len(q.Images)>0{params["images"]=q.Images}
                    encoded,encodeErr:=json.Marshal(params)
                    if encodeErr!=nil||len(encoded)>1000000{err=errors.New("message exceeds A2X frame limit");break}
                    var answer struct{Text string `json:"text"`}
                    err=client.Call(ctx,"task.send",params,&answer);value=answer.Text
                }
            default:err=errors.New("unknown desktop request")
            }
            stop()
            reply:=map[string]any{"id":q.ID,"value":value}
            if err!=nil{reply["error"]=err.Error()}
            if err=emit(reply);err!=nil{return err}
        }
    }}
}
