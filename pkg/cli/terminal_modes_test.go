package cli

import (
    "bytes"
    "strings"
    "testing"
)
func TestTerminalModesScopedRecovery(t *testing.T){
    tests:=[]struct{name,input,want string}{
        {"EOF nested pushes","\x1b[>7u\x1b[>3u","\x1b[>7u\x1b[>3u\x1b[<2u"},
        {"normal exit no double pop","\x1b[>7u\x1b[<u","\x1b[>7u\x1b[<1u"},
        {"cannot pop host stack","\x1b[>7u\x1b[<999u\x1b[<u","\x1b[>7u\x1b[<1u"},
        {"host direct keyboard mutation blocked","\x1b[=7u",""},
        {"unknown mouse baseline preserved","\x1b[?1006h\x1b[?1006l",""},
        {"opaque OSC","\x1b]title;\x1b[>7u\a","\x1b]title;\x1b[>7u\a"},
        {"opaque DCS","\x1bPdata\x1b[>7u\x1b\\","\x1bPdata\x1b[>7u\x1b\\"},
        {"unfinished CSI discarded","\x1b[>7u\x1b[?1006","\x1b[>7u\x1b[<1u"},
        {"unfinished OSC cancelled","\x1b[>7u\x1b]unfinished","\x1b[>7u\x1b]unfinished\x18\x1b\\\x1b[<1u"},
        {"screen stack balanced before leaving","\x1b[?1049h\x1b[>7u\x1b[?1049l","\x1b[?1049s\x1b[?1049h\x1b[>7u\x1b[<1u\x1b[?1049l\x1b[?1049r"},
    }
    for _,tt:=range tests{t.Run(tt.name,func(t *testing.T){
        for step:=1;step<=len(tt.input);step++{
            var out bytes.Buffer;m:=newTerminalModes(&out,nil)
            for i:=0;i<len(tt.input);i+=step{end:=i+step;if end>len(tt.input){end=len(tt.input)};if _,err:=m.Write([]byte(tt.input[i:end]));err!=nil{t.Fatal(err)}}
            if err:=m.Restore();err!=nil{t.Fatal(err)};if err:=m.Restore();err!=nil{t.Fatal(err)}
            if out.String()!=tt.want{t.Fatalf("split %d got %q want %q",step,out.String(),tt.want)}
        }
    })}
}
func TestTerminalModifierPreservesBaseline(t *testing.T){
    for _,baseline:=range []int{0,1,2}{
        var out bytes.Buffer;m:=newTerminalModes(&out,&baseline)
        m.Write([]byte("\x1b[>4;2m"));m.Restore()
        if !strings.HasSuffix(out.String(),string([]byte{27,'[','>','4',';',byte('0'+baseline),'m'})){t.Fatalf("baseline %d not restored: %q",baseline,out.String())}
    }
    var out bytes.Buffer;m:=newTerminalModes(&out,nil);m.Write([]byte("\x1b[>4;2m"));m.Restore();if out.Len()!=0{t.Fatal("unqueryable host modifier mode changed")}
}

func TestTerminalInputModesWithoutPrivateSaveRestore(t *testing.T){
    for _,supported:=range []bool{false,true}{for _,initial:=range []bool{false,true}{
        var out bytes.Buffer;m:=newTerminalModes(&out,nil);m.privateBaseline=map[int]bool{}
        if supported{for _,n:=range []int{1000,1004,1006,2004}{m.privateBaseline[n]=initial}}
        for _,part:=range []string{"\x1b[?1000;100", "4;1006;2004h"}{if _,err:=m.Write([]byte(part));err!=nil{t.Fatal(err)}}
        if err:=m.Restore();err!=nil{t.Fatal(err)};before:=out.String();m.Restore();if out.String()!=before{t.Fatal("double restore")}
        if !supported{if out.Len()!=0{t.Fatal("unknown baseline mutated",out.String())};continue}
        suffix:="l";if initial{suffix="h"}
        for _,n:=range []string{"1000","1004","1006","2004"}{if !strings.Contains(out.String(),"\x1b[?"+n+suffix){t.Fatal("baseline not restored",out.String())};if strings.Contains(out.String(),"\x1b[?"+n+"s"){t.Fatal("relied on private save")}}
    }}
}
