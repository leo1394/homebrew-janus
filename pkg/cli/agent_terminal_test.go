package cli

import (
    "context"
    "encoding/base64"
    "encoding/json"
    "io"
    "net"
    "os"
    "os/exec"
    "strings"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/a2x"
)
func TestAgentTerminalPTYHelper(t *testing.T){
    mode:=os.Getenv("JANUS_PTY_TEST");if mode==""{return}
    left,right:=net.Pipe();client,err:=a2x.New(left,[]a2x.SharedTool{{Tool:a2x.Tool{Name:"approved",InputSchema:json.RawMessage(`{"type":"object"}`)},Call:func(context.Context,a2x.ToolCall)(a2x.ToolResult,error){return a2x.ToolResult{Content:[]a2x.Content{{Type:"text",Text:"local text"}}},nil}}},nil);if err!=nil{t.Fatal(err)};defer client.Close()
    serverDone:=make(chan error,1)
    go func(){
        defer right.Close();dec:=json.NewDecoder(right);enc:=json.NewEncoder(right)
        respond:=func(id json.RawMessage,result any)error{return enc.Encode(map[string]any{"jsonrpc":"2.0","id":id,"result":result})}
        notify:=func(method string,params any)error{return enc.Encode(map[string]any{"jsonrpc":"2.0","method":method,"params":params})}
        resized,input,toolReply:=false,false,false
        for {var m struct{ID json.RawMessage;Method string;Params json.RawMessage;Result json.RawMessage};if err:=dec.Decode(&m);err!=nil{serverDone<-err;return}
            switch m.Method {
            case "":if strings.Contains(string(m.Result),"local text"){toolReply=true}else{t.Error("tool call did not survive terminal session",string(m.Result))}
            case "initialize":if err:=respond(m.ID,map[string]any{"terminal":true});err!=nil{serverDone<-err;return}
            case "session.open":if err:=respond(m.ID,map[string]string{"sessionId":"s"});err!=nil{serverDone<-err;return}
            case "terminal.open":
                var options a2x.TerminalOptions;json.Unmarshal(m.Params,&options);if options.Term!="xterm-256color"||options.ColorTerm!="truecolor"||!options.NoColor{t.Error("terminal environment not forwarded",options)}
                if mode=="reject"{enc.Encode(map[string]any{"jsonrpc":"2.0","id":m.ID,"error":map[string]any{"code":-32000,"message":"rejected"}});serverDone<-nil;return}
                if mode=="eof"||mode=="cancel"{
                    notify("terminal.data",map[string]string{"sessionId":"s","data":base64.StdEncoding.EncodeToString([]byte("\x1b[>4;2m\x1b[>7u\x1b[?1000;1006;1004;2004hEOF_READY"))})
                    respond(m.ID,map[string]any{});if mode=="eof"{serverDone<-nil;return};continue
                }
                // Output before the open response must be consumed, never deadlock.
                notify("terminal.data",map[string]string{"sessionId":"s","data":base64.StdEncoding.EncodeToString([]byte("\x1b[31mREADY\x1b[0m\r\n"))})
                if err:=respond(m.ID,map[string]any{});err!=nil{serverDone<-err;return}
                enc.Encode(map[string]any{"jsonrpc":"2.0","id":"tool-1","method":"clientTool.call","params":map[string]any{"sessionId":"s","name":"approved","arguments":map[string]any{}}})
            case "terminal.resize":
                if len(m.ID)!=0{t.Error("resize is not notification")}
                var p struct{Cols,Rows int};json.Unmarshal(m.Params,&p);if p.Cols==100&&p.Rows==40{resized=true}
            case "terminal.input":
                if len(m.ID)!=0{t.Error("input is not notification")}
                var p struct{Data string};json.Unmarshal(m.Params,&p);b,_:=base64.StdEncoding.DecodeString(p.Data);if strings.Contains(string(b),"x"){input=true}
            }
            if resized&&input&&toolReply{notify("terminal.exit",map[string]any{"sessionId":"s","code":0});serverDone<-nil;return}
        }
    }()
    ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
    if err:=client.Initialize(ctx);err!=nil{t.Fatal(err)};if _,err:=client.Open(ctx,"");err!=nil{t.Fatal(err)}
    err=runAgentTerminal(ctx,client,os.Stdin,os.Stdout)
    if mode=="reject"||mode=="eof"||mode=="cancel"{if err==nil{t.Fatal("rejected open succeeded")}}else if err!=nil{t.Fatal(err)}
    client.Close();if err:=<-serverDone;err!=nil&&err!=io.EOF{t.Fatal(err)}
}
func TestAgentTerminalRawRestoreAndResize(t *testing.T){
    if _,err:=exec.LookPath("python3");err!=nil{t.Skip("python3 is required for PTY integration fixture")}
    executable,err:=os.Executable();if err!=nil{t.Fatal(err)}
    script:=`import os,pty,subprocess,sys,termios,fcntl,struct,select,time,signal
for mode in ['normal','reject','eof','cancel']:
 master,slave=pty.openpty()
 fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',24,80,0,0))
 before=termios.tcgetattr(slave)
 env=os.environ.copy();env['JANUS_PTY_TEST']=mode;env['TERM']='xterm-256color';env['COLORTERM']='truecolor';env['NO_COLOR']=''
 process=subprocess.Popen([sys.argv[1],'-test.run=^TestAgentTerminalPTYHelper$'],stdin=slave,stdout=slave,stderr=slave,env=env)
 output=b'';sent=False;queried=False;deadline=time.monotonic()+10
 while process.poll() is None and time.monotonic()<deadline:
  ready,_,_=select.select([master],[],[],0.05)
  if ready: output+=os.read(master,65536)
  if b'\x1b[?4m' in output and not queried:
   os.write(master,b'\x1b[>4;1m');queried=True
  if mode=='cancel' and b'EOF_READY' in output and not sent:
   os.kill(process.pid,signal.SIGTERM);sent=True
  if mode not in ['cancel','eof'] and b'READY' in output and not sent:
   fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',40,100,0,0))
   os.kill(process.pid,signal.SIGWINCH);os.write(master,b'x');sent=True
 if process.poll() is None: process.kill();raise AssertionError('PTY test timed out')
 while select.select([master],[],[],0)[0]: output+=os.read(master,65536)
 assert process.returncode==0,(mode,output)
 if mode in ['eof','cancel']:
  assert b'\x1b[>4;1m' in output,('modifier baseline not restored',output)
  assert b'\x1b[<1u' in output,('kitty stack leaked after EOF',output)
  for number in [1000,1006,1004,2004]:
   assert ('\x1b[?%ds'%number).encode() not in output,('unsupported private save emitted',number,output)
  assert b'\x1b[?1000;1006;1004;2004h' not in output,('unqueryable modes enabled',output)
 after=termios.tcgetattr(slave)
 # Darwin sets PENDIN when canonical mode is restored with queued input.
 # It is kernel transient state, not an application raw/echo setting.
 after[3]&=~termios.PENDIN;before[3]&=~termios.PENDIN
 assert after==before,('terminal state not restored',mode,before,after,output)
 if mode=='normal': assert b'\x1b[31mREADY\x1b[0m\r\n' in output,output
 os.close(master);os.close(slave)
`
    command:=exec.Command("python3","-c",script,executable)
    if output,err:=command.CombinedOutput();err!=nil{t.Fatal(string(output),err)}
}
