package cli

import (
    "errors"
    "fmt"
    "os"
    "strconv"
    "strings"

    "github.com/spf13/cobra"
    "golang.org/x/term"
    "github.com/leo1394/homebrew-janus/internal/selector"
    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

func runDeviceBatch(cmd *cobra.Command,s store.Store,args []string,operation func(janus.Profile)error)error{
    rows,err:=s.List();if err!=nil{return err}
    return runTargetBatch(cmd,rows,args,operation)
}
func runTargetBatch(cmd *cobra.Command,rows []janus.Profile,args []string,operation func(janus.Profile)error)error{
    if len(args)==0{
        if len(rows)==0{return errors.New("no SSH devices; run 'janus spawn user@host'")}
        candidates:=make([]selector.Candidate,0,len(rows))
        for _,p:=range rows{candidates=append(candidates,selector.Candidate{Name:p.Name,Path:displayID(p.ID),Detail:p.DeviceType()+" · "+store.Target(p)})}
        chosen,ok,err:=selector.SelectMany(cmd.Context(),os.Stdin,cmd.ErrOrStderr(),selector.Prompt{Title:"Select devices to "+cmd.Name(),EmptySelectionNotice:"Select at least one device before confirming."},candidates)
        if err!=nil{return fmt.Errorf("device selector: %w; specify IDs or names when non-interactive",err)}
        if !ok{return errors.New("device selection cancelled")}
        for _,candidate:=range chosen{args=append(args,candidate.Path)}
    }
    succeeded,failed,unmatched,skipped:=0,0,0,0
    seen:=map[string]bool{}
    for _,arg:=range args{
        p,err:=resolveBatchTarget(rows,arg)
        if err!=nil{unmatched++;fmt.Fprintf(cmd.ErrOrStderr(),"Unmatched: %q: %v\n",arg,err);continue}
        if seen[p.ID]{skipped++;continue};seen[p.ID]=true
        if err:=operation(p);err!=nil{failed++;fmt.Fprintf(cmd.ErrOrStderr(),"Failed: %s: %v\n",p.Name,err);continue}
        succeeded++;fmt.Fprintf(cmd.OutOrStdout(),"%s: %s — done\n",cmd.Name(),p.Name)
    }
    color:=false;if f,ok:=cmd.OutOrStdout().(*os.File);ok{_,noColor:=os.LookupEnv("NO_COLOR");color=term.IsTerminal(int(f.Fd()))&&!noColor&&os.Getenv("TERM")!="dumb"}
    fmt.Fprintf(cmd.OutOrStdout(),"Total: %s  Succeeded: %s  Failed: %s  Unmatched: %s  Duplicates skipped: %s\n",
        batchNumber(len(args),"36",color),batchNumber(succeeded,"32",color),batchNumber(failed,"31",color),batchNumber(unmatched,"33",color),batchNumber(skipped,"33",color))
    if failed+unmatched>0{return errors.New("batch completed with unsuccessful devices")}
    return nil
}
func batchNumber(n int,code string,color bool)string{
    text:=strconv.Itoa(n);if color{return "\x1b[1;"+code+"m"+text+"\x1b[0m"};return text
}

func resolveBatchTarget(rows []janus.Profile,arg string)(janus.Profile,error){
    for _,p:=range rows{if strings.EqualFold(p.ID,arg)||strings.EqualFold(displayID(p.ID),arg)||p.Platform=="credential"&&p.ID==store.CanonicalAccountID(arg){return p,nil}}
    matches:=[]janus.Profile{};for _,p:=range rows{if strings.EqualFold(p.Name,arg){matches=append(matches,p)}}
    if len(matches)==1{return matches[0],nil};if len(matches)>1{return janus.Profile{},fmt.Errorf("ambiguous name %q; use a full ID",arg)}
    return janus.Profile{},fmt.Errorf("unknown target %q",arg)
}
