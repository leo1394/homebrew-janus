package upgrade

import (
    "context"
    "bytes"
    "errors"
    "os/exec"
    "github.com/leo1394/homebrew-janus/internal/metadata"
    "io"
    "net/http"
    "strings"
    "testing"
)
type roundTrip func(*http.Request)(*http.Response,error)
func (f roundTrip) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
func TestVersionOrdering(t *testing.T){
    for _,tc:=range []struct{a,b string;want int}{{"1.9.0","v1.10.0",-1},{"v1.10.0","1.9.0",1},{"1.0.0","1.0.0",0},{"2.0.0","1.99.99",1}}{got,err:=compare(tc.a,tc.b);if err!=nil||got!=tc.want{t.Fatal(tc,got,err)}}
    for _,bad:=range []string{"0.1.0-dev","1.0","01.0.0","1.0.0-rc1","999999999999999999999.0.0"}{if _,err:=parse(bad);err==nil{t.Fatal("invalid version accepted",bad)}}
}
func TestConfirmationDefaultsToNo(t *testing.T){
    for _,tc:=range []struct{in string;yes bool}{{"y\n",true},{"YES\n",true},{" yes \n",true},{"n\n",false},{"\n",false},{"",false},{"maybe\n",false},{"yes",false}}{if got:=confirm(strings.NewReader(tc.in));got!=tc.yes{t.Fatal(tc,got)}}
}
func TestReleaseMetadata(t *testing.T){
    for _,tc:=range []struct{body string;status int;ok bool}{{`{"tag_name":"v1.10.0"}`,200,true},{`{"tag_name":"v1.0.0-rc1","prerelease":true}`,200,false},{`{"tag_name":"v1.0.0","draft":true}`,200,false},{`{`,200,false},{`{}`,404,false}}{
        client:=&http.Client{Transport:roundTrip(func(r *http.Request)(*http.Response,error){return &http.Response{StatusCode:tc.status,Body:io.NopCloser(strings.NewReader(tc.body))},nil})}
        got,err:=latest(context.Background(),client,"https://example.invalid/latest");if (err==nil)!=tc.ok{t.Fatal(tc,got,err)}
    }
}

func TestUpgradeOutcomes(t *testing.T){
    cases:=[]struct{name,current,next,input string;interactive,yes bool;runError,verifyError bool;wantRun bool;wantError bool;want string}{
        {name:"current",current:"1.10.0",next:"1.10.0",want:"already on the latest"},
        {name:"ahead",current:"1.11.0",next:"1.10.0",want:"no downgrade"},
        {name:"development",current:"0.1.0-dev",next:"1.10.0",wantError:true,want:"development build"},
        {name:"noninteractive",current:"1.9.0",next:"1.10.0",wantError:true,want:"rerun interactively"},
        {name:"no",current:"1.9.0",next:"1.10.0",input:"no\n",interactive:true,want:"cancelled"},
        {name:"empty",current:"1.9.0",next:"1.10.0",input:"\n",interactive:true,want:"cancelled"},
        {name:"EOF",current:"1.9.0",next:"1.10.0",interactive:true,want:"cancelled"},
        {name:"invalid",current:"1.9.0",next:"1.10.0",input:"maybe\n",interactive:true,want:"cancelled"},
        {name:"yes",current:"1.9.0",next:"1.10.0",input:"YES\n",interactive:true,wantRun:true,want:"Successfully upgraded"},
        {name:"flag",current:"1.9.0",next:"1.10.0",yes:true,wantRun:true,want:"Successfully upgraded"},
        {name:"failed install",current:"1.9.0",next:"1.10.0",yes:true,runError:true,wantRun:true,wantError:true,want:"upgrade failed"},
        {name:"failed verify",current:"1.9.0",next:"1.10.0",yes:true,verifyError:true,wantRun:true,wantError:true,want:"verification failed"},
    }
    for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){
        calls:=0
        d:=testDependencies(tc.current,tc.next)
        d.interactive=func()bool{return tc.interactive}
        d.run=func(c *exec.Cmd)error{calls++;if c.Args[0]!="/bin/bash"||!strings.Contains(strings.Join(c.Args," "),"--version 1.10.0"){t.Fatal(c.Args)};if tc.runError{return errors.New("download checksum mismatch")};return nil}
        d.output=func(c *exec.Cmd)([]byte,error){if tc.verifyError{return nil,errors.New("cannot execute installed version")};return []byte(metadata.DisplayName+" version "+tc.next+" (2026-11-19)\n"+metadata.Repository+"\n"),nil}
        cmd:=command(d);var out bytes.Buffer;cmd.SetOut(&out);cmd.SetErr(&out);cmd.SetIn(strings.NewReader(tc.input));if tc.yes{cmd.SetArgs([]string{"--yes"})}else{cmd.SetArgs([]string{})}
        err:=cmd.Execute();text:=out.String();if err!=nil{text+=err.Error()};if (err!=nil)!=tc.wantError||!strings.Contains(text,tc.want)||(calls>0)!=tc.wantRun{t.Fatal(text,err,calls)}
    })}
}
func testDependencies(current,next string)dependencies{
    return dependencies{version:current,client:&http.Client{Transport:roundTrip(func(*http.Request)(*http.Response,error){return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(`{"tag_name":"v`+next+`"}`))},nil})},executable:func()(string,error){return "/fixture/bin/janus",nil},owner:func(string)(target,error){return target{prefix:"/fixture",script:"/fixture/share/janus/install.sh"},nil},interactive:func()bool{return false},run:func(*exec.Cmd)error{return nil},output:func(*exec.Cmd)([]byte,error){return nil,errors.New("unexpected output call")},lookPath:func(string)(string,error){return "",errors.New("not on PATH")}}
}
func TestUpgradeMetadataFailureNeverMeansLatest(t *testing.T){
    d:=testDependencies("1.0.0","1.1.0");d.client=&http.Client{Transport:roundTrip(func(*http.Request)(*http.Response,error){return nil,errors.New("offline")})}
    cmd:=command(d);var out bytes.Buffer;cmd.SetOut(&out);cmd.SetArgs([]string{})
    if err:=cmd.Execute();err==nil||!strings.Contains(err.Error(),"release check failed")||strings.Contains(out.String(),"latest version"){t.Fatal(out.String(),err)}
}
func TestHomebrewLagAndTargetChanges(t *testing.T){
    for _,available:=range []string{"1.0.0","1.3.0"}{t.Run(available,func(t *testing.T){
        d:=testDependencies("1.0.0","1.2.0");d.owner=func(string)(target,error){return target{brew:"/fixture/brew",formula:"owner/tap/janus"},nil};updates:=0
        d.run=func(c *exec.Cmd)error{if len(c.Args)>1&&c.Args[1]=="upgrade"{updates++};return nil}
        d.output=func(*exec.Cmd)([]byte,error){return []byte(`{"formulae":[{"versions":{"stable":"`+available+`"}}]}`),nil}
        cmd:=command(d);cmd.SetArgs([]string{"--yes"});err:=cmd.Execute();if err==nil||updates!=0{t.Fatal(err,updates)}
    })}
}
