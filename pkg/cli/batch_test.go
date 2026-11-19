package cli

import (
    "bytes"
    "context"
    "errors"
    "path/filepath"
    "strings"
    "testing"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

type batchRuntime struct{
    janus.SSHRuntime
    calls []string
    failID string
}
func(r *batchRuntime)Disconnect(_ context.Context,p janus.Profile)error{
    r.calls=append(r.calls,p.ID);if p.ID==r.failID{return errors.New("fixture failure")};return nil
}
func(r *batchRuntime)DeleteCredential(ctx context.Context,p janus.Profile)error{return r.Disconnect(ctx,p)}

func TestBatchContinuesAndDeduplicates(t *testing.T){
    for _,command:=range []string{"disconnect","remove"}{t.Run(command,func(t *testing.T){
        s:=store.Store{Root:filepath.Join(t.TempDir(),"base")}
        first,_,err:=s.Add("alice","one.example",22);if err!=nil{t.Fatal(err)}
        bad,_,err:=s.Add("alice","two.example",22);if err!=nil{t.Fatal(err)}
        last,_,err:=s.Add("alice","three.example",22);if err!=nil{t.Fatal(err)}
        r:=&batchRuntime{failID:bad.ID}
        cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
        var output bytes.Buffer;cmd.SetOut(&output);cmd.SetErr(&output)
        cmd.SetArgs([]string{command,"missing",first.Name,displayID(first.ID),bad.Name,last.ID})
        if err:=cmd.Execute();err==nil{t.Fatal("partial failure must return nonzero")}
        if strings.Join(r.calls,",")!=strings.Join([]string{first.ID,bad.ID,last.ID},","){t.Fatal("batch stopped or repeated an operation",r.calls)}
        if !strings.Contains(output.String(),"Total: 5  Succeeded: 2  Failed: 1  Unmatched: 1  Duplicates skipped: 1"){t.Fatal(output.String())}
        rows,err:=s.List();if err!=nil{t.Fatal(err)}
        if command=="remove"{if len(rows)!=1||rows[0].ID!=bad.ID{t.Fatal("failed credential deletion must preserve device",rows)}}else if len(rows)!=3{t.Fatal("disconnect removed records",rows)}
    })}
}
func TestBatchSuccessAndRepeatedArgumentCompletion(t *testing.T){
    s:=store.Store{Root:filepath.Join(t.TempDir(),"base")}
    first,_,err:=s.Add("alice","one.example",22);if err!=nil{t.Fatal(err)}
    second,_,err:=s.Add("alice","two.example",22);if err!=nil{t.Fatal(err)}
    r:=&batchRuntime{}
    cmd,err:=New(Options{Store:s,Runtime:r});if err!=nil{t.Fatal(err)}
    var output bytes.Buffer;cmd.SetOut(&output);cmd.SetErr(&output);cmd.SetArgs([]string{"disconnect",first.ID,second.ID})
    if err:=cmd.Execute();err!=nil{t.Fatal(err)}
    if len(r.calls)!=2||!strings.Contains(output.String(),"Succeeded: 2  Failed: 0"){t.Fatal(output.String(),r.calls)}
    for _,command:=range []string{"disconnect","remove"}{out,err:=execute(t,s,"__complete",command,first.Name,"alice@two");if err!=nil||!strings.Contains(out,second.Name){t.Fatal(out,err)}}
}
func TestBatchNumbersHonorColor(t *testing.T){
    if batchNumber(2,"32",true)!="\x1b[1;32m2\x1b[0m"||batchNumber(2,"32",false)!="2"{t.Fatal("incorrect summary highlighting")}
}
