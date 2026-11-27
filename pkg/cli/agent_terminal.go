package cli

import (
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "os/signal"
    "regexp"
    "strconv"
    "syscall"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
    "github.com/spf13/cobra"
    "golang.org/x/sys/unix"
    "golang.org/x/term"
)
func agentTTY(cmd *cobra.Command)(*os.File,*os.File,bool){
    in,inOK:=cmd.InOrStdin().(*os.File);out,outOK:=cmd.OutOrStdout().(*os.File)
    return in,out,inOK&&outOK&&term.IsTerminal(int(in.Fd()))&&term.IsTerminal(int(out.Fd()))
}
// Poll instead of leaving a goroutine blocked in stdin after remote exit. The
// caller owns stdin; never close it or change its file-status flags.
func readTerminal(ctx context.Context,in *os.File,ch chan<- []byte,failed chan<- error){
    buffer:=make([]byte,8192)
    for ctx.Err()==nil{
        fds:=[]unix.PollFd{{Fd:int32(in.Fd()),Events:unix.POLLIN}}
        count,err:=unix.Poll(fds,100);if err==unix.EINTR{continue};if err!=nil{select{case failed<-err:case <-ctx.Done():};return};if count==0{continue}
        n,err:=unix.Read(int(in.Fd()),buffer)
        if n>0{data:=append([]byte(nil),buffer[:n]...);select{case ch<-data:case <-ctx.Done():return}}
        if err!=nil||n==0{if err==nil{err=io.EOF};select{case failed<-err:case <-ctx.Done():};return}
    }
}
// Query only the modifier mode without changing it. Unsupported terminals time
// out quickly; in that case remote modifyOtherKeys mutations are not forwarded.
// Preserve all unrelated input, including keystrokes typed during the query.
func terminalModifierBaseline(in,out *os.File)(*int,[]byte){
    if _,err:=out.Write([]byte("\x1b[?4m"));err!=nil{return nil,nil}
    response:=regexp.MustCompile(`\x1b\[>4;([0-9]+)m`)
    pending:=[]byte{};deadline:=time.Now().Add(150*time.Millisecond)
    for time.Now().Before(deadline){
        fds:=[]unix.PollFd{{Fd:int32(in.Fd()),Events:unix.POLLIN}}
        n,err:=unix.Poll(fds,10);if err==unix.EINTR{continue};if err!=nil{break};if n==0{continue}
        buffer:=make([]byte,1024);n,err=unix.Read(int(in.Fd()),buffer);if err!=nil||n==0{break};pending=append(pending,buffer[:n]...)
        if match:=response.FindSubmatchIndex(pending);match!=nil{
            value,err:=strconv.Atoi(string(pending[match[2]:match[3]]));if err!=nil{break}
            rest:=append([]byte(nil),pending[:match[0]]...);rest=append(rest,pending[match[1]:]...);return &value,rest
        }
        if len(pending)>8192{break}
    }
    return nil,pending
}
// Never infer xterm private save/restore support from TERM. Terminal.app can
// enable focus/mouse reports while ignoring both save/restore and DECRQM.
func terminalInputBaseline(in,out *os.File)(map[int]bool,[]byte){
    baseline:=map[int]bool{};query:=""
    for _,n:=range []int{9,1000,1001,1002,1003,1004,1005,1006,1007,1015,1016,2004}{query+=fmt.Sprintf("\x1b[?%d$p",n)}
    if _,err:=io.WriteString(out,query);err!=nil{return baseline,nil}
    response:=regexp.MustCompile(`\x1b\[\?([0-9]+);([0-4])\$y`)
    pending:=[]byte{};deadline:=time.Now().Add(150*time.Millisecond)
    for time.Now().Before(deadline){
        fds:=[]unix.PollFd{{Fd:int32(in.Fd()),Events:unix.POLLIN}}
        n,err:=unix.Poll(fds,10);if err==unix.EINTR{continue};if err!=nil{break};if n==0{continue}
        buffer:=make([]byte,1024);n,err=unix.Read(int(in.Fd()),buffer);if err!=nil||n==0{break};pending=append(pending,buffer[:n]...)
        for {match:=response.FindSubmatchIndex(pending);if match==nil{break}
            mode,_:=strconv.Atoi(string(pending[match[2]:match[3]]));value,_:=strconv.Atoi(string(pending[match[4]:match[5]]))
            if terminalInputMode(mode)&&(value==1||value==2){baseline[mode]=value==1}
            pending=append(pending[:match[0]],pending[match[1]:]...)
        }
        if len(pending)>8192{break}
    }
    return baseline,pending
}
func runAgentTerminal(ctx context.Context,client *a2x.Client,in,out *os.File)(err error){
    events,err:=client.TerminalEvents();if err!=nil{return err}
    cols,rows,err:=term.GetSize(int(out.Fd()));if err!=nil{return err}
    state,err:=term.MakeRaw(int(in.Fd()));if err!=nil{return err}
    defer func(){err=errors.Join(err,term.Restore(int(in.Fd()),state))}()
    ctx,cancel:=signal.NotifyContext(ctx,syscall.SIGTERM,syscall.SIGHUP,syscall.SIGINT);defer cancel()
    otherKeys,earlyInput:=terminalModifierBaseline(in,out)
    privateBaseline,privateInput:=terminalInputBaseline(in,out);earlyInput=append(earlyInput,privateInput...)
    modes:=newTerminalModes(out,otherKeys);modes.privateBaseline=privateBaseline;defer func(){err=errors.Join(err,modes.Restore())}()
    stopOnCancel:=context.AfterFunc(ctx,func(){client.Close()});defer stopOnCancel()
    // Closing the A2X connection also cancels any pending local tool calls.
    defer client.Close()
    resize:=make(chan os.Signal,1);signal.Notify(resize,syscall.SIGWINCH);defer signal.Stop(resize)
    stopLoading:=startAgentLoading(out);defer stopLoading()
    opened:=make(chan error,1)
    go func(){request,done:=context.WithTimeout(ctx,30*time.Second);defer done();_,noColor:=os.LookupEnv("NO_COLOR");opened<-client.OpenTerminalWithOptions(request,a2x.TerminalOptions{Cols:cols,Rows:rows,Term:os.Getenv("TERM"),ColorTerm:os.Getenv("COLORTERM"),NoColor:noColor})}()
    input:=make(chan []byte,1);failed:=make(chan error,1);readerDone:=make(chan struct{})
    readerStarted:=false
    defer func(){cancel();if readerStarted{<-readerDone}}()
    var transportErr error
    failedWrite:=func(err error){transportErr=err;input=nil;resize=nil;client.Close()}
    var exitCode *int
    finish:=func(code int)error{if code!=0{return fmt.Errorf("remote terminal exited with status %d",code)};return nil}
    for {
        select {
        case err:=<-opened:
            if err!=nil{return err};opened=nil
            if exitCode!=nil{return finish(*exitCode)}
            if len(earlyInput)>0{if err:=client.TerminalInput(earlyInput);err!=nil{return err}}
            readerStarted=true;inputStream:=input;go func(){defer close(readerDone);readTerminal(ctx,in,inputStream,failed)}()
            if c,r,e:=term.GetSize(int(out.Fd()));e==nil{if err:=client.TerminalResize(c,r);err!=nil{failedWrite(err)}}
        case event:=<-events:
            if len(event.Data)>0{stopLoading();if _,err:=modes.Write(event.Data);err!=nil{return err}}
            if event.Exit{exitCode=&event.Code;if opened==nil{return finish(event.Code)}}
        case data:=<-input:
            if err:=client.TerminalInput(data);err!=nil{failedWrite(err)}
        case err:=<-failed:return err
        case <-resize:
            if opened==nil{if c,r,e:=term.GetSize(int(out.Fd()));e==nil{if err:=client.TerminalResize(c,r);err!=nil{failedWrite(err)}}}
        case <-ctx.Done():return ctx.Err()
        case <-client.Done():
            // Drain already-decoded data/exit before reporting transport closure.
            for {select{case event:=<-events:if len(event.Data)>0{stopLoading();if _,err:=modes.Write(event.Data);err!=nil{return err}};if event.Exit{exitCode=&event.Code};default:if exitCode!=nil{return finish(*exitCode)};if transportErr!=nil{return transportErr};return client.Err()}}
        }
    }
}
