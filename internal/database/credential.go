package database

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "time"
    "strconv"

    "github.com/leo1394/homebrew-janus/internal/store"
)

func HelperPath()(string,error){
    executable,err:=os.Executable();if err!=nil{return "",err};executable,err=filepath.EvalSymlinks(executable);if err!=nil{return "",err}
    helper:=filepath.Join(filepath.Dir(filepath.Dir(executable)),"libexec","janus-keychain")
    info,err:=os.Stat(helper);if err!=nil||info.Mode().Perm()&0111==0{return "",errors.New("native Keychain helper is unavailable; rebuild or reinstall Janus")};return helper,nil
}

// Password is owner-process-only. It never accepts a caller-selected helper path.
// The helper independently verifies the exact live signed parent and output pipe.
func Password(ctx context.Context,a store.Account,namespace string,enroll bool,tlsMode ...string)([]byte,error){
    operation:="read";if enroll{operation="enroll"}
    return credentialOperation(ctx,a,namespace,operation,tlsMode...)
}
func DeleteCredential(ctx context.Context,a store.Account,namespace string)error{secret,err:=credentialOperation(ctx,a,namespace,"delete");for i:=range secret{secret[i]=0};return err}
func credentialOperation(ctx context.Context,a store.Account,namespace,operation string,tlsMode ...string)([]byte,error){
    helper,err:=HelperPath();if err!=nil{return nil,err}
    credentialID:=a.CredentialID;if credentialID==""{credentialID=a.ID}
    mode:="verify";if len(tlsMode)>0&&tlsMode[0]!=""{mode=tlsMode[0]}
    request,err:=json.Marshal(map[string]string{"accountId":credentialID,"host":a.Host,"user":a.User,"port":strconv.Itoa(a.Port),"operation":operation,"namespace":namespace,"tlsMode":mode,"provider":a.Provider,"environment":a.Environment});if err!=nil{return nil,err}
    bounded,cancel:=context.WithTimeout(ctx,190*time.Second);defer cancel()
    cmd:=exec.CommandContext(bounded,helper,"--database-credential");cmd.Stdin=bytes.NewReader(request)
    // Do not forward native diagnostics into caller logs or return values.
    cmd.Stderr=io.Discard
    pipe,err:=cmd.StdoutPipe();if err!=nil{return nil,errors.New("cannot create private credential channel")}
    if err:=cmd.Start();err!=nil{return nil,errors.New("cannot start native credential approval")}
    secret,readErr:=io.ReadAll(io.LimitReader(pipe,16385));if len(secret)>16384{cancel()}
    waitErr:=cmd.Wait()
    if readErr!=nil||waitErr!=nil||len(secret)>16384||operation!="delete"&&len(secret)==0{for i:=range secret{secret[i]=0};return nil,errors.New("native database credential operation canceled or failed")}
    return secret,nil
}
