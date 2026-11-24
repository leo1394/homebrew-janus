package ssh

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "errors"
    "os"
    "time"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

// Recovery never changes or deletes the old item. A new slot is selected only
// after password-only SSH authentication and a ready, independently owned master.
func(r *Runtime)RecoverCredential(ctx context.Context,p janus.Profile)error{
    if p.IsAgent(){return errors.New("credential recovery currently requires an SSH terminal device")}
    if err:=store.Validate(p);err!=nil{return err}
    helper,err:=r.helper();if err!=nil{return err}
    var token [32]byte;if _,err:=rand.Read(token[:]);err!=nil{return err}
    staged:=p;staged.CredentialSlot=hex.EncodeToString(token[:])
    directory,err:=os.MkdirTemp("/private/tmp","janus-recover-");if err!=nil{return err};defer os.RemoveAll(directory)
    candidate:=*r;candidate.Store=store.Store{Root:directory};candidate.Helper=helper;candidate.AgentControlSocket=""
    if err:=candidate.Store.Save([]janus.Profile{staged});err!=nil{return err}
    // Stop only this disposable verification master. Never stop the user's master.
    defer func(){cleanup,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel();_ = candidate.Disconnect(cleanup,staged)}()
    if err:=candidate.Ensure(ctx,staged);err!=nil{return errors.New("recovery verification failed; original credential selection retained (unselected staged Keychain item may remain)")}
    unlock,err:=r.Store.Lock("registry");if err!=nil{return err};defer unlock()
    rows,err:=r.Store.List();if err!=nil{return err};found:=false
    for _,row:=range rows{if row.ID==p.ID{if row!=p{return errors.New("device changed during recovery; original selection retained")};found=true}}
    if !found{return errors.New("device removed during recovery; no selection changed")}
    for i:=range rows{if store.Target(rows[i])==store.Target(p){rows[i].CredentialSlot=staged.CredentialSlot}}
    return r.Store.Save(rows)
}
