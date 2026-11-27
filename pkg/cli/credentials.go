package cli

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "sort"
    "path/filepath"
    "strings"
    "text/tabwriter"

    "github.com/spf13/cobra"
    "github.com/leo1394/homebrew-janus/internal/database"
    "github.com/leo1394/homebrew-janus/internal/cliprovider"
    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

func credentialCommand(s store.Store,r janus.SSHRuntime,db database.Runtime)*cobra.Command{
    var recover,edit,remove,asJSON bool
    var add,environment,username string
    command:=&cobra.Command{Use:"credential [--recover|--edit|--remove] [ID|NAME]",Short:"Manage credentials by account or device ID/name.",Long:"List saved accounts and manage credentials. Omit ID|NAME with an operation to select an eligible account or device interactively.\n\n--recover accepts an account or device ID/name. SSH and Agent devices recover their SSH credentials; MySQL devices resolve to their linked account. Replacement credentials are verified before selection; the old Keychain item is retained.\n--edit accepts only a MySQL or CLI provider account ID/name; it replaces the account password after verification.\n--remove deletes an unreferenced account and its selected Keychain credential. Use janus remove ID|NAME to remove an account or device.",Example:"  janus credential --recover\n  janus credential --edit\n  janus credential --remove\n  janus credential --recover macOS-Runner\n  janus credential --recover ACCOUNT_ID\n  janus credential --edit ACCOUNT_ID\n  janus remove ACCOUNT_OR_DEVICE_ID",Args:cobra.MaximumNArgs(1),RunE:func(cmd *cobra.Command,args []string)error{
        if add!=""{
            if add!="clobotics"||len(args)!=0{return errors.New("use credential --add clobotics --environment ENV --username USER")}
            account,err:=s.PrepareCLIAccount(username,environment);if err!=nil{return err}
            secret,err:=database.Password(cmd.Context(),account,db.Namespace,true);if err!=nil{return err}
            client:=cliprovider.New(account);defer client.Close();err=client.Login(cmd.Context(),secret);for i:=range secret{secret[i]=0};if err!=nil{return err}
            if err:=s.RegisterCLIAccount(account);err!=nil{return err};return json.NewEncoder(cmd.OutOrStdout()).Encode(cliprovider.Metadata(account))
        }
        if !recover&&!edit&&!remove{
            if len(args)>0{return errors.New("specify --recover, --edit or --remove")}
            rows,err:=s.Accounts();if err!=nil{return err};if asJSON{return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)}
            if len(rows)==0{fmt.Fprintln(cmd.OutOrStdout(),"No accounts saved. Add one with: janus spawn mysql USER@HOST");return nil}
            w:=tabwriter.NewWriter(cmd.OutOrStdout(),0,4,2,' ',0)
            fmt.Fprintln(w,"ID\tNAME\tPROVIDER\tUSER\tHOST\tPORT")
            for _,a:=range rows{fmt.Fprintf(w,"%s\t%s\t%s\t%s\t%s\t%d\n",a.ID,a.Name,a.Provider,a.User,a.Host,a.Port)};return w.Flush()
        }
        if len(args)==0{
            operation:="recover";if edit{operation="edit"};if remove{operation="remove"}
            rows,err:=s.List();if err!=nil{return err};accounts,err:=s.Accounts();if err!=nil{return err}
            title:="Select account to "+operation;if recover{title="Select account or device to "+operation}
            target,err:=chooseCredentialTarget(rows,accounts,operation,func(candidates []selector.Candidate)(selector.Candidate,bool,error){
                return selector.SelectOne(cmd.Context(),os.Stdin,cmd.ErrOrStderr(),selector.Prompt{Title:title,EmptySelectionNotice:"Select one target before confirming."},candidates)
            });if err!=nil{return err};args=[]string{target}
        }
        a,err:=s.Account(args[0])
        if err!=nil{
            if edit{return fmt.Errorf("--edit requires an account ID or name, not a device; run janus credential to list accounts: %w",err)}
            rows,listErr:=s.List();if listErr!=nil{return listErr};p,resolveErr:=resolve(rows,args[0]);if resolveErr!=nil{return err}
            if p.IsMySQL(){a,err=s.Account(p.AccountID);if err!=nil{return err}}else{
                if !recover{return errors.New("SSH devices support --recover; use remove for SSH device removal")}
                recovery,ok:=r.(interface{RecoverCredential(context.Context,janus.Profile)error});if !ok{return errors.New("runtime does not support credential recovery")}
                if err:=recovery.RecoverCredential(cmd.Context(),p);err!=nil{return err}
                fmt.Fprintln(cmd.OutOrStdout(),"Credential verified and selected. Previous Keychain item and active sessions are preserved.");return nil
            }
        }
        if remove{return s.RemoveAccount(a.ID,func(account store.Account)error{return database.DeleteCredential(cmd.Context(),account,db.Namespace)})}
        // Editing authentication material is a verified replacement, not an
        // in-place overwrite. Endpoint/user edits require a new target/account.
        var token [16]byte;if _,err:=rand.Read(token[:]);err!=nil{return err}
        replacement:=a;replacement.CredentialID="account:"+hex.EncodeToString(token[:]);replacement.Revision++
        if a.Provider=="clobotics"{
            secret,err:=database.Password(cmd.Context(),replacement,db.Namespace,true);if err!=nil{return err}
            client:=cliprovider.New(replacement);defer client.Close();err=client.Login(cmd.Context(),secret);for i:=range secret{secret[i]=0};if err!=nil{return err}
            if err:=s.ReplaceAccount(a,replacement);err!=nil{return err};fmt.Fprintln(cmd.OutOrStdout(),"CLI provider credential verified and replaced; old item retained.");return nil
        }
        rows,err:=s.List();if err!=nil{return err}
        target:=database.Target{Host:a.Host,Port:a.Port,User:a.User}
        for _,p:=range rows{if p.AccountID==a.ID{target.Database=p.Database;target.CAPEM=p.DatabaseCA;target.TLSMode=p.DatabaseTLS;break}}
        secret,err:=database.Password(cmd.Context(),replacement,db.Namespace,true,target.TLSMode);if err!=nil{return err}
        connection,err:=database.Open(cmd.Context(),target,secret);for i:=range secret{secret[i]=0}
        if err!=nil{return errors.New("replacement credential verification failed; original selection retained; staged Keychain item retained")};connection.Close()
        if err:=s.ReplaceAccount(a,replacement);err!=nil{return err}
        fmt.Fprintln(cmd.OutOrStdout(),"Credential verified and replaced. Old Keychain item retained; previous database sessions are revoked.");return nil
    }}
    command.Flags().StringVar(&add,"add","","Add a clobotics account using native password entry")
    command.Flags().StringVar(&environment,"environment","","CLI provider environment: dev, test, prod, prod-us")
    command.Flags().StringVar(&username,"username","","CLI provider username")
    command.Flags().BoolVar(&recover,"recover",false,"Recover credentials by account or device ID/name, preserving the old item")
    command.Flags().BoolVar(&edit,"edit",false,"Replace a MySQL or CLI provider account password after authentication verification")
    command.Flags().BoolVar(&remove,"remove",false,"Delete an unreferenced account and its selected Keychain credential")
    command.Flags().BoolVar(&asJSON,"json",false,"List public account metadata as JSON")
    command.MarkFlagsMutuallyExclusive("add","recover","edit","remove")
    command.MarkFlagsMutuallyExclusive("recover","edit","remove","json")
    command.ValidArgsFunction=func(cmd *cobra.Command,args []string,toComplete string)([]string,cobra.ShellCompDirective){out:=[]string{};if len(args)>0{return out,cobra.ShellCompDirectiveNoFileComp};accounts,_:=s.Accounts();for _,a:=range accounts{if strings.HasPrefix(a.Name,toComplete){out=append(out,a.Name)}};if edit{return out,cobra.ShellCompDirectiveNoFileComp};rows,_:=s.List();for _,p:=range rows{if strings.HasPrefix(p.Name,toComplete){out=append(out,p.Name)}};return out,cobra.ShellCompDirectiveNoFileComp}
    return command
}
// Keep picker eligibility specific to the operation; store methods recheck
// references and account revisions when performing the mutation.
func chooseCredentialTarget(rows []janus.Profile,accounts []store.Account,operation string,pick func([]selector.Candidate)(selector.Candidate,bool,error))(string,error){
    if operation!="recover"&&operation!="edit"&&operation!="remove"{return "",errors.New("unknown credential operation")}
    candidates:=[]selector.Candidate{};targets:=map[string]string{};referenced:=map[string]bool{};supported:=map[string]bool{}
    for _,a:=range accounts{supported[a.ID]=a.Provider=="mysql"||a.Provider=="clobotics"}
    rows=append([]janus.Profile(nil),rows...)
    sort.SliceStable(rows,func(i,j int)bool{return janus.PlatformOrder(rows[i].DevicePlatform())<janus.PlatformOrder(rows[j].DevicePlatform())})
    for _,p:=range rows{
        if p.AccountID!=""{referenced[p.AccountID]=true}
        eligible:=p.IsMySQL()&&supported[p.AccountID] || operation=="recover"&&(p.IsAgent()||p.DevicePlatform()=="terminal"&&p.DeviceType()=="ssh")
        if operation!="recover"||!eligible{continue}
        id:=displayID(p.ID);targets[id]=p.ID;candidates=append(candidates,selector.Candidate{Name:p.Name,Path:id,Detail:p.DeviceType()+" · "+store.Target(p)})
    }
    for _,a:=range accounts{
        if !supported[a.ID]||operation=="remove"&&referenced[a.ID]{continue}
        detail:=fmt.Sprintf("%s account · %s@%s:%d",a.Provider,a.User,a.Host,a.Port);if a.Environment!=""{detail+=" · "+a.Environment}
        targets[a.ID]=a.ID;candidates=append(candidates,selector.Candidate{Name:a.Name,Path:a.ID,Detail:detail})
    }
    kind:="accounts";if operation=="recover"{kind="accounts or devices"}
    if len(candidates)==0{return "",fmt.Errorf("no eligible %s for --%s",kind,operation)}
    selected,ok,err:=pick(candidates);if err!=nil{return "",fmt.Errorf("credential selector: %w; specify an ID or name from eligible %s when non-interactive",err,kind)}
    if !ok{return "",errors.New("credential selection cancelled")}
    target,ok:=targets[selected.Path];if !ok{return "",errors.New("selected credential target no longer exists")};return target,nil
}
func databaseSessionCommand(cmd *cobra.Command,r database.Runtime,method,id string)error{
    paths,err:=filepath.Glob(filepath.Join(r.Store.Root,"mysql","*.sock"));cliPaths,_:=filepath.Glob(filepath.Join(r.Store.Root,"cli","*.sock"));paths=append(paths,cliPaths...);if err!=nil{return err};all:=[]database.SessionInfo{}
    for _,path:=range paths{c,err:=database.Dial(cmd.Context(),path);if err!=nil{continue};reply,err:=c.Call(cmd.Context(),database.Request{Method:"list"});if err!=nil{c.Close();return err}
        if method=="list"{all=append(all,reply.Sessions...);c.Close();continue}
        for _,s:=range reply.Sessions{if s.ID==id{_,err:=c.Call(cmd.Context(),database.Request{Method:method,SessionID:id});c.Close();return err}};c.Close()
    }
    if method!="list"{return errors.New("unknown managed session")};return json.NewEncoder(cmd.OutOrStdout()).Encode(all)
}
