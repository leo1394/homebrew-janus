package upgrade

import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "context"
    "crypto/sha256"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/metadata"
)
func releaseFixture(t *testing.T,extra *tar.Header,corrupt bool)[]byte{
    t.Helper();var archive bytes.Buffer;compressed:=gzip.NewWriter(&archive);writer:=tar.NewWriter(compressed)
    files:=map[string][]byte{"bin/janus":[]byte("binary"),"libexec/janus-keychain":[]byte("helper"),"share/janus/install.sh":[]byte("installer")}
    manifest:="";for name,data:=range files{manifest+=fmt.Sprintf("%x  %s\n",sha256.Sum256(data),name)}
    if corrupt{files["bin/janus"]=[]byte("tampered")};files["SHA256SUMS"]=[]byte(manifest)
    for name,data:=range files{if err:=writer.WriteHeader(&tar.Header{Name:name,Mode:0700,Size:int64(len(data))});err!=nil{t.Fatal(err)};if _,err:=writer.Write(data);err!=nil{t.Fatal(err)}}
    if extra!=nil{if err:=writer.WriteHeader(extra);err!=nil{t.Fatal(err)}}
    if err:=writer.Close();err!=nil{t.Fatal(err)};if err:=compressed.Close();err!=nil{t.Fatal(err)};return archive.Bytes()
}
func TestVerifiedReleasePayload(t *testing.T){
    for _,tc:=range []struct{name string;extra *tar.Header;corrupt bool;valid bool}{
        {name:"valid",valid:true},{name:"corrupt",corrupt:true},
        {name:"traversal",extra:&tar.Header{Name:"../escape",Mode:0700}},
        {name:"symlink",extra:&tar.Header{Name:"libexec/janus-keychain-migrate",Typeflag:tar.TypeSymlink,Linkname:"/tmp/escape"}},
        {name:"duplicate",extra:&tar.Header{Name:"bin/janus",Mode:0700}},
    }{t.Run(tc.name,func(t *testing.T){
        dir:=t.TempDir();err:=unpackRelease(releaseFixture(t,tc.extra,tc.corrupt),dir)
        if (err==nil)!=tc.valid{t.Fatal(err)}
    })}
    payload:=[]byte("release");line:=fmt.Sprintf("%x  asset\n",sha256.Sum256(payload))
    for _,manifest:=range []string{line,"",line+line,strings.ReplaceAll(line,"asset","other")}{
        err:=verifyChecksum(payload,[]byte(manifest),"asset");if (err==nil)!=(manifest==line){t.Fatal(manifest,err)}
    }
}
func TestStandaloneOwnershipAndAdoption(t *testing.T){
    if runtime.GOOS!="darwin"{t.Skip("macOS native installation")};prefix,err:=filepath.EvalSymlinks(t.TempDir());if err!=nil{t.Fatal(err)};binary:=filepath.Join(prefix,"bin","janus")
    if err:=os.MkdirAll(filepath.Dir(binary),0700);err!=nil{t.Fatal(err)};if err:=os.WriteFile(binary,[]byte("binary"),0700);err!=nil{t.Fatal(err)}
    target,err:=ownership(binary);if err!=nil||!target.adopt||target.prefix!=prefix{t.Fatal(target,err)}
    marker:=filepath.Join(prefix,"share","janus","install.json");if err:=os.MkdirAll(filepath.Dir(marker),0700);err!=nil{t.Fatal(err)}
    for _,repository:=range []string{"",metadata.Repository,"https://example.invalid/other"}{
        text:=fmt.Sprintf(`{"method":"standalone","prefix":%q,"version":"1.0.0","repository":%q}`,prefix,repository)
        if err:=os.WriteFile(marker,[]byte(text),0600);err!=nil{t.Fatal(err)}
        target,err:=ownership(binary);if (err==nil)!=(repository==""||repository==metadata.Repository){t.Fatal(repository,target,err)}
    }
    if err:=os.Remove(marker);err!=nil{t.Fatal(err)};if err:=os.WriteFile(filepath.Join(prefix,"go.mod"),[]byte("module source"),0600);err!=nil{t.Fatal(err)}
    if _,err:=ownership(binary);err==nil{t.Fatal("source build adopted")}
}
func TestPostUpgradeMetadata(t *testing.T){
    valid:=metadata.DisplayName+" version 1.2.0 (2026-11-27)\n"+metadata.Repository+"\n"
    for _,text:=range []string{valid,strings.ReplaceAll(valid,metadata.Repository,"https://example.invalid"),valid+"extra\n",strings.ReplaceAll(valid,"1.2.0","1.3.0"),strings.ReplaceAll(valid,"2026-11-27","bad")} {
        if err:=verifyVersionOutput(text,"1.2.0");(err==nil)!=(text==valid){t.Fatal(text,err)}
    }
}
func TestVerifiedPreparationOnlyAfterApproval(t *testing.T){
    for _,yes:=range []bool{false,true}{
        d:=testDependencies("1.0.0","1.2.0");prepared:=0;ran:=false
        d.prepare=func(context.Context,string)(string,func(),error){prepared++;return "",nil,fmt.Errorf("checksum mismatch")}
        d.run=func(*exec.Cmd)error{ran=true;return nil}
        cmd:=command(d);cmd.SetOut(&bytes.Buffer{});cmd.SetErr(&bytes.Buffer{});cmd.SetArgs([]string{});if yes{cmd.SetArgs([]string{"--yes"})}
        if err:=cmd.Execute();err==nil{t.Fatal("unexpected success")}
        if ran||(yes&&prepared!=1)||(!yes&&prepared!=0){t.Fatal(yes,prepared,ran)}
    }
}
func TestStandaloneUsesConfirmedPayloadAndCleansUp(t *testing.T){
    d:=testDependencies("1.0.0","1.2.0");cleaned:=false;ran:=false
    d.prepare=func(_ context.Context,v string)(string,func(),error){if v!="1.2.0"{t.Fatal(v)};return "/fixture/stage",func(){cleaned=true},nil}
    d.run=func(cmd *exec.Cmd)error{
        ran=true;want:="/bin/bash /fixture/stage/share/janus/install.sh --local /fixture/stage --version 1.2.0 --prefix /fixture"
        if strings.Join(cmd.Args," ")!=want{t.Fatal(cmd.Args)};return nil
    }
    d.output=func(*exec.Cmd)([]byte,error){return []byte(metadata.DisplayName+" version 1.2.0 (2026-11-27)\n"+metadata.Repository+"\n"),nil}
    cmd:=command(d);cmd.SetOut(&bytes.Buffer{});cmd.SetArgs([]string{"--yes"})
    if err:=cmd.Execute();err!=nil||!ran||!cleaned{t.Fatal(err,ran,cleaned)}
}
