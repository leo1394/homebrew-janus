package cli

import (
    "bytes"
    "context"
    "github.com/leo1394/homebrew-janus/pkg/janus"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/metadata"
    "github.com/leo1394/homebrew-janus/internal/store"
)
func execute(t *testing.T,s store.Store,args ...string)(string,error){t.Helper();cmd,err:=New(Options{Store:s});if err!=nil{t.Fatal(err)};var out bytes.Buffer;cmd.SetOut(&out);cmd.SetErr(&out);cmd.SetArgs(args);err=cmd.Execute();return out.String(),err}
func TestHelpVersionAndCompletionWithoutState(t *testing.T){
    root:=filepath.Join(t.TempDir(),"absent");s:=store.Store{Root:root}
    for _,args:=range [][]string{{"version"},{"--version"}}{out,err:=execute(t,s,args...);if err!=nil||out!=metadata.VersionText()||strings.Count(out,"\n")!=2{t.Fatal(out,err)}}
    for _,args:=range [][]string{{"help"},{"--help"},{"help","spawn"},{"spawn","--help"},{"help","disconnect"},{"upgrade","--help"},{"completion","zsh"},{"completion","bash"},{"completion","fish"},{"__complete","help",""}}{out,err:=execute(t,s,args...);if err!=nil||len(out)==0{t.Fatal(args,out,err)}}
    if _,err:=os.Stat(root);!os.IsNotExist(err){t.Fatal("help created state",err)}
    if _,err:=execute(t,s,"terminal");err==nil{t.Fatal("terminal alias exists")}
}
func TestCompletionReadOnlyAndQuotedDeviceNames(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)};if err:=s.Rename(p.ID,"Build Server");err!=nil{t.Fatal(err)}
    before,_:=os.ReadFile(filepath.Join(s.Root,"devices.json"))
    for _,command:=range []string{"connect","disconnect","remove","rename"}{out,err:=execute(t,s,"__complete",command,"Bu");if err!=nil||!strings.Contains(out,"Build Server")||!strings.Contains(out,":4"){t.Fatal(command,out,err)}}
    out,err:=execute(t,s,"__complete","spawn","");if err!=nil||!strings.Contains(out,":4"){t.Fatal(out,err)}
    out,err=execute(t,s,"__complete","completion","");if err!=nil||!strings.Contains(out,"fish"){t.Fatal(out,err)}
    out,err=execute(t,s,"__complete","rename","Build Server","");if err!=nil||strings.Contains(out,"Build Server"){t.Fatal(out,err)}
    after,_:=os.ReadFile(filepath.Join(s.Root,"devices.json"));if !bytes.Equal(before,after){t.Fatal("completion mutated registry")}
}
func TestInvalidInputsAndSuggestions(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"absent")}
    for _,args:=range [][]string{{"devices","--list","--json"},{"rename","one"},{"spawn","-p","0","alice@host"},{"connect","one","two"},{"devices","--jsno"}}{if _,err:=execute(t,s,args...);err==nil{t.Fatal("invalid input accepted",args)}}
    _,err:=execute(t,s,"versoin");if err==nil||!strings.Contains(err.Error(),"version"){t.Fatal(err)}
}
func TestManualMatchesDistributedFile(t *testing.T){data,err:=os.ReadFile("../../man/janus.1");if err!=nil||string(data)!=Manual{t.Fatal("manual drift",err)}}
func TestNoConfidentialCommandsInBase(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"absent")}
    for _,name:=range []string{"terminal","add","install","sessions","confidential"}{if _,err:=execute(t,s,name);err==nil{t.Fatal("out-of-scope command enabled",name)}}
    _,err:=execute(t,s,"devices","--jsno");if err==nil||!strings.Contains(err.Error(),"Did you mean '--json'"){t.Fatal(err)}
}
type extraProvider struct{}
func(extraProvider)Devices(context.Context)([]janus.Device,error){return []janus.Device{{ID:"future:one",Name:"Future Device",Platform:"android",Type:"adb"}},nil}
func TestExplicitDeviceProviderExtension(t *testing.T){
    cmd,err:=New(Options{Store:store.Store{Root:filepath.Join(t.TempDir(),"absent")},Providers:[]janus.DeviceProvider{extraProvider{}}});if err!=nil{t.Fatal(err)}
    var out bytes.Buffer;cmd.SetOut(&out);cmd.SetArgs([]string{"devices","--json"});if err:=cmd.Execute();err!=nil{t.Fatal(err)}
    if !strings.Contains(out.String(),`"platform":"android"`){t.Fatal(out.String())}
}
func TestEmptyDevicesMessage(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"absent")}
    out,err:=execute(t,s,"devices");if err!=nil||!strings.Contains(out,"No devices saved")||strings.Contains(out,"CONNECTED"){t.Fatal(out,err)}
    out,err=execute(t,s,"devices","--json");if err!=nil||out!="[]\n"{t.Fatal(out,err)}
}

func TestRemoveWithoutDeviceUsesSelection(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"absent")}
    _,err:=execute(t,s,"remove");if err==nil||!strings.Contains(err.Error(),"no SSH devices"){t.Fatal("remove did not reach device selection",err)}
}

func TestCompletionHiddenFromTopLevelHelp(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"absent")}
    out,err:=execute(t,s,"--help");if err!=nil||strings.Contains(out,"completion"){t.Fatal(out,err)}
    for _,shell:=range []string{"bash","zsh","fish"}{out,err:=execute(t,s,"completion",shell);if err!=nil||out==""{t.Fatal(shell,out,err)}}
}
