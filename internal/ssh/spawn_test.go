package ssh

import (
    "reflect"
    "bytes"
    "os/exec"
    "strings"
    "testing"
)
func TestSSHArgumentNormalization(t *testing.T){
    for _,tc:=range []struct{in,want []string}{
        {[]string{"user@host","-p","3233"},[]string{"-p","3233","user@host"}},
        {[]string{"-p","3233","user@host","echo","hello world"},[]string{"-p","3233","user@host","echo","hello world"}},
        {[]string{"-L","8080:localhost:80","user@host"},[]string{"-L","8080:localhost:80","user@host"}},
    }{if got:=normalize(tc.in);!reflect.DeepEqual(got,tc.want){t.Fatal(got,tc.want)}}
}
func TestResolvedSSHProfile(t *testing.T){
    p,err:=resolvedProfile("host alias\nuser builder\nhostname 192.0.2.15\nport 3233\n");if err!=nil||p.User!="builder"||p.Port!=3233||p.Host!="192.0.2.15"{t.Fatal(p,err)}
}
func TestConfigQueryDoesNotRequestTTY(t *testing.T){
    cmd:=exec.Command("/usr/bin/ssh","-G","-T","fixture@127.0.0.1")
    var stderr bytes.Buffer;cmd.Stderr=&stderr
    if _,err:=cmd.Output();err!=nil{t.Fatal(err,stderr.String())}
    if strings.Contains(stderr.String(),"Pseudo-terminal"){t.Fatal(stderr.String())}
}
