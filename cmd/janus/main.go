package main

import (
    "fmt"
    "os"
    "strings"
    "github.com/leo1394/homebrew-janus/pkg/cli"
)
func main(){
    root,err:=cli.New(cli.Options{})
    if err==nil{err=root.Execute()}
    if err!=nil{
        fmt.Fprintln(os.Stderr,"Error:",err)
        if strings.Contains(err.Error(),"unknown flag:") || strings.Contains(err.Error(),"unknown shorthand flag:") { fmt.Fprintln(os.Stderr,"Check option spelling with 'janus help <command>'.") }
        fmt.Fprintln(os.Stderr,"Run 'janus help' for usage.")
        os.Exit(1)
    }
}
