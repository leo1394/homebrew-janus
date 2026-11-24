package cli

import (
    "fmt"
    "bufio"
    "io"
    "strconv"
    "strings"
)

// terminalModes observes complete control sequences, including those split over
// A2X frames. String controls (OSC/DCS/APC/PM) are opaque, not nested CSI streams.
// Input-report modes require a queried baseline; DEC save/restore alone is not portable.
type terminalModes struct {
    out *bufio.Writer
    pending []byte
    state byte
    stringEscape bool
    kitty int
    saved map[int]bool
    order []int
    otherKeys *int
    otherKeysChanged bool
    privateBaseline map[int]bool
}
func newTerminalModes(out io.Writer,otherKeys *int)*terminalModes{
    return &terminalModes{out:bufio.NewWriter(out),saved:make(map[int]bool),otherKeys:otherKeys}
}
func(m *terminalModes)emit(s string)error{_,err:=io.WriteString(m.out,s);return err}
func(m *terminalModes)keyboardRestore()error{
    if m.kitty==0{return nil};n:=m.kitty;m.kitty=0;return m.emit(fmt.Sprintf("\x1b[<%du",n))
}
func terminalPrivateMode(n int)bool{
    switch n{case 1,9,25,47,1000,1001,1002,1003,1004,1005,1006,1007,1015,1016,1047,1049,2004,2026:return true};return false
}
func terminalInputMode(n int)bool{
    switch n{case 9,1000,1001,1002,1003,1004,1005,1006,1007,1015,1016,2004:return true};return false
}
func(m *terminalModes)sequence(s string)error{
    body:=s[2:len(s)-1];final:=s[len(s)-1]
    if final=='u'&&len(body)>0{
        switch body[0]{
        case '>':
            // Bound our contribution to the terminal's finite keyboard stack.
            if m.kitty>=16{return fmt.Errorf("remote keyboard mode stack limit exceeded")};m.kitty++
        case '<':
            n:=1;if len(body)>1{v,e:=strconv.Atoi(body[1:]);if e!=nil||v<1{return nil};n=v}
            if n>m.kitty{n=m.kitty};if n==0{return nil};m.kitty-=n
            return m.emit(fmt.Sprintf("\x1b[<%du",n))
        case '=':
            // Direct mutations without a remote push would modify host state.
            // Require a scoped push; Pi uses the standard push/pop protocol.
            if m.kitty==0{return nil}
        }
    }
    if final=='m'&&(body==">4"||strings.HasPrefix(body,">4;")){
        if m.otherKeys==nil{return nil} // Unsupported query: preserve host state.
        m.otherKeysChanged=true
    }
    if len(body)>0&&body[0]=='?'&&(final=='h'||final=='l'||final=='s'||final=='r'){
        values:=strings.Split(body[1:],";")
        if (final=='h'||final=='l'){
            allowed:=[]string{}
            for _,v:=range values{
                n,e:=strconv.Atoi(v)
                if e==nil&&terminalInputMode(n){if _,known:=m.privateBaseline[n];!known{continue}}
                allowed=append(allowed,v)
            }
            if len(allowed)==0{return nil};values=allowed;s="\x1b[?"+strings.Join(values,";")+string(final)
        }
        for _,v:=range values{
            n,e:=strconv.Atoi(v);if e!=nil||!terminalPrivateMode(n){continue}
            if final=='s'||final=='r'{return nil} // Do not overwrite our saved baseline.
            // Kitty stacks are per screen. Balance before changing screens.
            if n==47||n==1047||n==1049{if err:=m.keyboardRestore();err!=nil{return err}}
            if !m.saved[n]{
                if !terminalInputMode(n){if err:=m.emit(fmt.Sprintf("\x1b[?%ds",n));err!=nil{return err}}
                m.saved[n]=true;m.order=append(m.order,n)
            }
        }
    }
    return m.emit(s)
}
func(m *terminalModes)Write(data []byte)(n int,err error){
    defer func(){if e:=m.out.Flush();err==nil{err=e}}()
    for i,b:=range data{
        switch m.state{
        case 0:
            if b==27{m.pending=append(m.pending,b);m.state=1}else{if _,err:=m.out.Write([]byte{b});err!=nil{return i,err}}
        case 1:
            m.pending=append(m.pending,b)
            if b=='['{m.state=2;continue}
            if b==']'||b=='P'||b=='_'||b=='^'{m.state=3;m.stringEscape=false}else{m.state=0}
            if _,err:=m.out.Write(m.pending);err!=nil{return i,err};m.pending=nil
        case 2:
            if b==27{m.pending=[]byte{27};m.state=1;continue}
            m.pending=append(m.pending,b)
            if len(m.pending)>1024{return i,fmt.Errorf("remote terminal control sequence too long")}
            if b>=0x40&&b<=0x7e{s:=string(m.pending);m.pending=nil;m.state=0;if err:=m.sequence(s);err!=nil{return i,err}}
        case 3:
            if _,err:=m.out.Write([]byte{b});err!=nil{return i,err}
            if b==7||(m.stringEscape&&b=='\\'){m.state=0};m.stringEscape=b==27
        }
    }
    return len(data),nil
}
func(m *terminalModes)Restore()(err error){
    defer func(){if e:=m.out.Flush();err==nil{err=e}}()
    // Cancel an unterminated string control before emitting recovery sequences.
    if m.state==3{if err:=m.emit("\x18\x1b\\");err!=nil{return err}}
    m.state=0;m.pending=nil
    if err:=m.keyboardRestore();err!=nil{return err}
    if m.otherKeysChanged{m.otherKeysChanged=false;if err:=m.emit(fmt.Sprintf("\x1b[>4;%dm",*m.otherKeys));err!=nil{return err}}
    for i:=len(m.order)-1;i>=0;i--{
        n:=m.order[i];sequence:=fmt.Sprintf("\x1b[?%dr",n)
        if terminalInputMode(n){final:="l";if m.privateBaseline[n]{final="h"};sequence=fmt.Sprintf("\x1b[?%d%s",n,final)}
        if err:=m.emit(sequence);err!=nil{return err}
    }
    m.order=nil;m.saved=make(map[int]bool)
    return nil
}
