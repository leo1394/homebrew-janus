package cli

import (
    "bytes"
    "context"
    "errors"
    "reflect"
    "path/filepath"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

type credentialRuntime struct{janus.SSHRuntime; recovered string}
func(r *credentialRuntime)RecoverCredential(_ context.Context,p janus.Profile)error{r.recovered=p.ID;return nil}
func TestCredentialRecoveryCommand(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)}
    for _,args:=range [][]string{{"credential","--recover",p.Name},{"credential",p.Name},{"credential","--recover=false",p.Name},{"credential","--recover"},{"recover-credential",p.Name}}{
        r:=&credentialRuntime{};cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)};var out bytes.Buffer;cmd.SetOut(&out);cmd.SetErr(&out);cmd.SetArgs(args);err=cmd.Execute()
        valid:=len(args)==3&&args[1]=="--recover"
        if valid{if err!=nil||r.recovered!=p.ID{t.Fatal(args,err,r.recovered)}}else if err==nil||r.recovered!=""{t.Fatal("invalid recovery invoked",args,err)}
    }
    out,err:=execute(t,s,"__complete","credential","--recover","ali");if err!=nil||!strings.Contains(out,p.Name){t.Fatal(out,err)}
    out,err=execute(t,s,"credential","--help");if err!=nil||!strings.Contains(out,"--recover"){t.Fatal(out,err)}
}

func TestCredentialPickerEligibility(t *testing.T){
    rows:=[]janus.Profile{
        {ID:"database:db",Name:"db",Platform:"database",Type:"mysql",AccountID:"linked"},
        {ID:"agent:ai",Name:"ai",Platform:"a2x",Type:"pi"},
        {ID:"terminal:ssh",Name:"ssh"},
        {ID:"other",Platform:"ios",Type:"usb"},
    }
    accounts:=[]store.Account{{ID:"linked",Provider:"mysql"},{ID:"free",Provider:"mysql"},{ID:"cli",Provider:"clobotics",Environment:"test"},{ID:"unknown",Provider:"future"}}
    for operation,want:=range map[string][]string{"recover":{"ssh","ai","db","linked","free","cli"},"edit":{"linked","free","cli"},"remove":{"free","cli"}}{
        t.Run(operation,func(t *testing.T){
            target,err:=chooseCredentialTarget(rows,accounts,operation,func(candidates []selector.Candidate)(selector.Candidate,bool,error){
                got:=[]string{};for _,c:=range candidates{got=append(got,c.Path);if c.Detail==""{t.Fatal("missing device type/account details")}}
                if !reflect.DeepEqual(got,want){t.Fatalf("got %v, want %v",got,want)}
                return candidates[0],true,nil
            })
            if err!=nil||target==""{t.Fatal(target,err)}
        })
    }
    if rows[0].ID!="database:db"{t.Fatal("picker mutated caller order")}
}

func TestCredentialPickerCancellationAndFailures(t *testing.T){
    rows:=[]janus.Profile{{ID:"terminal:ssh",Name:"ssh"}}
    _,err:=chooseCredentialTarget(nil,nil,"recover",func([]selector.Candidate)(selector.Candidate,bool,error){t.Fatal("empty picker invoked");return selector.Candidate{},false,nil})
    if err==nil{t.Fatal("empty registry accepted")}
    for _,mode:=range []string{"cancel","error","invalid"}{
        target,err:=chooseCredentialTarget(rows,nil,"recover",func([]selector.Candidate)(selector.Candidate,bool,error){
            if mode=="error"{return selector.Candidate{},false,errors.New("not a terminal")}
            return selector.Candidate{Path:"unknown"},mode=="invalid",nil
        })
        if err==nil||target!=""{t.Fatal(mode,target,err)}
    }
}

func TestCredentialPickerSSHRecoveryUsesDeviceID(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")};p,_,err:=s.Add("alice","example.com",22);if err!=nil{t.Fatal(err)}
    target,err:=chooseCredentialTarget([]janus.Profile{p},nil,"recover",func(c []selector.Candidate)(selector.Candidate,bool,error){return c[0],true,nil});if err!=nil{t.Fatal(err)}
    r:=&credentialRuntime{};cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
    cmd.SetArgs([]string{"credential","--recover",target});cmd.SetOut(&bytes.Buffer{});cmd.SetErr(&bytes.Buffer{})
    if err:=cmd.Execute();err!=nil||r.recovered!=p.ID{t.Fatal(err,r.recovered)}
}

func TestCredentialEditRejectsDeviceAndOmitsDeviceCompletion(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")}
    p,a,_,err:=s.PrepareMySQL("alice","db.example.com",3306,"","","verify");if err!=nil{t.Fatal(err)}
    p,err=s.RegisterMySQL(p,a);if err!=nil{t.Fatal(err)}
    if err:=s.Rename(p.ID,"mysql-device");err!=nil{t.Fatal(err)}
    for _,target:=range []string{p.ID,displayID(p.ID),"mysql-device"}{
        _,err:=execute(t,s,"credential","--edit",target)
        if err==nil||!strings.Contains(err.Error(),"--edit requires an account ID or name"){t.Fatal(target,err)}
    }
    out,err:=execute(t,s,"__complete","credential","--edit","mysql-");if err!=nil||strings.Contains(out,"mysql-device"){t.Fatal(out,err)}
    out,err=execute(t,s,"__complete","credential","--edit","ali");if err!=nil||!strings.Contains(out,a.Name){t.Fatal(out,err)}
    current,err:=s.Account(a.ID);if err!=nil||current!=a{t.Fatal("device edit changed account",err)}
    out,err=execute(t,s,"credential","--help");if err!=nil||strings.Contains(out,"MYSQL_DEVICE_ID")||!strings.Contains(out,"--edit accepts only"){t.Fatal(out,err)}
}
