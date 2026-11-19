package cli

import (
    "fmt"
    "sort"
    "strings"
    "github.com/spf13/cobra"
    "github.com/spf13/pflag"
)
func flagError(cmd *cobra.Command,err error)error{
    text:=err.Error();prefix:="unknown flag: --"
    if !strings.HasPrefix(text,prefix){return err}
    unknown:=strings.TrimPrefix(text,prefix);best:=3;candidates:=[]string{}
    visit:=func(f *pflag.Flag){d:=distance(unknown,f.Name);if d<best{best=d;candidates=[]string{f.Name}}else if d==best{candidates=append(candidates,f.Name)}}
    cmd.Flags().VisitAll(visit);cmd.InheritedFlags().VisitAll(visit)
    if len(candidates)==0||best>2{return err};sort.Strings(candidates)
    return fmt.Errorf("%w\nDid you mean '--%s'?\nRun 'janus help %s' for options.",err,candidates[0],cmd.Name())
}
func distance(a,b string)int{
    row:=make([]int,len(b)+1);for j:=range row{row[j]=j}
    for i:=1;i<=len(a);i++{prev:=row[0];row[0]=i;for j:=1;j<=len(b);j++{old:=row[j];cost:=0;if a[i-1]!=b[j-1]{cost=1};v:=prev+cost;if row[j]+1<v{v=row[j]+1};if row[j-1]+1<v{v=row[j-1]+1};row[j]=v;prev=old}}
    return row[len(b)]
}
