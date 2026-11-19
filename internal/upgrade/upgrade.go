package upgrade

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
    "time"

    "github.com/spf13/cobra"
    "golang.org/x/term"
    "github.com/leo1394/homebrew-janus/internal/metadata"
)

type version [3]uint64
var semver = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
func parse(value string)(version,error){
    parts:=semver.FindStringSubmatch(value);if parts==nil{return version{},fmt.Errorf("not a stable release version: %q",value)}
    var v version
    for i:=range v{n,err:=strconv.ParseUint(parts[i+1],10,64);if err!=nil{return v,err};v[i]=n};return v,nil
}
func compare(current,latest string)(int,error){
    a,err:=parse(current);if err!=nil{return 0,err};b,err:=parse(latest);if err!=nil{return 0,err}
    for i:=range a{if a[i]<b[i]{return -1,nil};if a[i]>b[i]{return 1,nil}};return 0,nil
}
type release struct{Tag string `json:"tag_name"`;Prerelease bool `json:"prerelease"`;Draft bool `json:"draft"`}
func latest(ctx context.Context,client *http.Client,url string)(string,error){
    request,err:=http.NewRequestWithContext(ctx,"GET",url,nil);if err!=nil{return "",err};request.Header.Set("Accept","application/vnd.github+json")
    response,err:=client.Do(request);if err!=nil{return "",fmt.Errorf("release check failed: %w",err)};defer response.Body.Close()
    if response.StatusCode!=200{return "",fmt.Errorf("release check failed: HTTP %d (the project may have no published release)",response.StatusCode)}
    var r release;if err:=json.NewDecoder(io.LimitReader(response.Body,1<<20)).Decode(&r);err!=nil{return "",fmt.Errorf("invalid release metadata: %w",err)}
    if r.Prerelease||r.Draft{return "",errors.New("latest metadata is not an eligible stable release")};if _,err:=parse(r.Tag);err!=nil{return "",err};return strings.TrimPrefix(r.Tag,"v"),nil
}
func confirm(in io.Reader)bool{line,err:=bufio.NewReader(in).ReadString('\n');if err!=nil{return false};answer:=strings.ToLower(strings.TrimSpace(line));return answer=="y"||answer=="yes"}

type installation struct{Method string `json:"method"`;Prefix string `json:"prefix"`;Version string `json:"version"`}
type target struct{prefix,script,brew,formula string}
func ownership(executable string)(target,error){
    resolved,err:=filepath.EvalSymlinks(executable);if err!=nil{return target{},err}
    if strings.Contains(resolved,"/Cellar/") {
        brew,err:=exec.LookPath("brew");if err!=nil{return target{},errors.New("Homebrew executable unavailable; use brew upgrade janus manually")}
        ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
        output,err:=exec.CommandContext(ctx,brew,"list","--full-name","--formula").Output();if err!=nil{return target{},fmt.Errorf("cannot determine Homebrew ownership: %w",err)}
        for _,formula:=range strings.Fields(string(output)){
            if formula!="janus"&&!strings.HasSuffix(formula,"/janus"){continue}
            cellar,err:=exec.CommandContext(ctx,brew,"--cellar",formula).Output();if err!=nil{continue}
            base:=strings.TrimSpace(string(cellar))+string(os.PathSeparator)
            if strings.HasPrefix(resolved,base){return target{brew:brew,formula:formula},nil}
        }
        return target{},errors.New("ambiguous Homebrew ownership; upgrade using the owning package manager")
    }
    prefix:=filepath.Dir(filepath.Dir(resolved))
    data,err:=os.ReadFile(filepath.Join(prefix,"share","janus","install.json"));if err!=nil{return target{},errors.New("source or unsupported installation; rebuild with make build, then run ./install.sh --local build --prefix PREFIX")}
    var record installation
    if json.Unmarshal(data,&record)!=nil||record.Method!="standalone"||record.Prefix!=prefix||resolved!=filepath.Join(prefix,"bin","janus"){return target{},errors.New("ambiguous installation ownership; reinstall explicitly")}
    script:=filepath.Join(prefix,"share","janus","install.sh")
    if info,err:=os.Stat(script);err!=nil||!info.Mode().IsRegular(){return target{},errors.New("standalone updater missing; reinstall explicitly")}
    return target{prefix:prefix,script:script},nil
}
type dependencies struct {
    version string
    client *http.Client
    executable func()(string,error)
    owner func(string)(target,error)
    interactive func()bool
    run func(*exec.Cmd)error
    output func(*exec.Cmd)([]byte,error)
    lookPath func(string)(string,error)
}
func Command()*cobra.Command{
    return command(dependencies{version:metadata.Version,client:&http.Client{Timeout:15*time.Second},executable:os.Executable,owner:ownership,interactive:func()bool{return term.IsTerminal(int(os.Stdin.Fd()))},run:func(c *exec.Cmd)error{return c.Run()},output:func(c *exec.Cmd)([]byte,error){return c.Output()},lookPath:exec.LookPath})
}
func command(d dependencies)*cobra.Command{
    var yes bool
    cmd:=&cobra.Command{Use:"upgrade",Short:"Check stable releases and upgrade an owned installation.",Long:"Check GitHub's latest stable release over HTTPS. Updates require confirmation (or --yes). Homebrew installations stay with their owning Formula; standalone installs reuse the verified installer. Source builds require a manual rebuild. No implicit downgrade; non-interactive input never approves an update.",Args:cobra.NoArgs,Example:"  janus upgrade\n  janus upgrade --yes"}
    cmd.Flags().BoolVar(&yes,"yes",false,"Approve an available upgrade without an interactive prompt")
    cmd.RunE=func(cmd *cobra.Command,args []string)error{
        next,err:=latest(cmd.Context(),d.client,"https://api.github.com/repos/leo1394/homebrew-janus/releases/latest");if err!=nil{return err}
        ordering,err:=compare(d.version,next);if err!=nil{return fmt.Errorf("development build requires an explicit rebuild/install: %w",err)}
        if ordering==0{fmt.Fprintf(cmd.OutOrStdout(),"Congrats! You're already on the latest version of janus (which is v%s)\n",next);return nil}
        if ordering>0{fmt.Fprintf(cmd.OutOrStdout(),"Installed janus v%s is newer than stable v%s; no downgrade performed.\n",d.version,next);return nil}
        executable,err:=d.executable();if err!=nil{return err};destination,err:=d.owner(executable);if err!=nil{return err}
        if destination.brew!=""{
            update:=exec.CommandContext(cmd.Context(),destination.brew,"update");update.Stdout=cmd.OutOrStdout();update.Stderr=cmd.ErrOrStderr();if err:=d.run(update);err!=nil{return fmt.Errorf("Homebrew metadata refresh failed: %w",err)}
            available,err:=brewVersion(cmd.Context(),destination,d.output);if err!=nil{return err}
            n,err:=compare(available,next);if err!=nil{return err};if n<0{return fmt.Errorf("v%s is not available through %s yet (Formula has v%s)",next,destination.formula,available)};if n>0{return errors.New("Formula target changed beyond the checked release; rerun upgrade")}
        }
        fmt.Fprintf(cmd.OutOrStdout(),"v%s is out! You're currently on v%s.\n",next,d.version)
        if destination.brew!=""{fmt.Fprintf(cmd.OutOrStdout(),"Method: Homebrew (%s), target v%s\n",destination.formula,next)}else{fmt.Fprintf(cmd.OutOrStdout(),"Method: standalone (%s), target v%s\n",destination.prefix,next)}
        if !yes {
            if !d.interactive(){return fmt.Errorf("update available; rerun interactively or use 'janus upgrade --yes'")}
            fmt.Fprint(cmd.OutOrStdout(),"==> Do you want to proceed with the upgrade? [y/N] ")
            if !confirm(cmd.InOrStdin()){fmt.Fprintln(cmd.OutOrStdout(),"Upgrade cancelled.");return nil}
        }
        var update *exec.Cmd
        if destination.brew!=""{
            available,err:=brewVersion(cmd.Context(),destination,d.output);if err!=nil{return err};if available!=next{return errors.New("Formula target changed; rerun upgrade for a new confirmation")}
            update=exec.CommandContext(cmd.Context(),destination.brew,"upgrade",destination.formula)
            update.Env=append(os.Environ(),"HOMEBREW_NO_AUTO_UPDATE=1")
        }else{update=exec.CommandContext(cmd.Context(),"/bin/bash",destination.script,"--version",next,"--prefix",destination.prefix)}
        update.Stdout=cmd.OutOrStdout();update.Stderr=cmd.ErrOrStderr();if err:=d.run(update);err!=nil{return fmt.Errorf("upgrade failed: %w; inspect the installation before retrying",err)}
        verify:=executable
        if destination.brew!=""{prefix,err:=d.output(exec.CommandContext(cmd.Context(),destination.brew,"--prefix",destination.formula));if err!=nil{return fmt.Errorf("resolve upgraded Formula: %w",err)};verify=filepath.Join(strings.TrimSpace(string(prefix)),"bin","janus")}
        output,err:=d.output(exec.CommandContext(cmd.Context(),verify,"version"));if err!=nil{return fmt.Errorf("post-upgrade verification failed: %w",err)}
        if !strings.HasPrefix(string(output),"janus version "+next+" ("){return errors.New("installed executable version does not match confirmed target")}
        if active,err:=d.lookPath("janus");err==nil{a,_:=filepath.EvalSymlinks(active);b,_:=filepath.EvalSymlinks(verify);if a!=b{return fmt.Errorf("v%s installed at %s, but PATH selects %s; update PATH before using the new version",next,verify,active)}}
        fmt.Fprintf(cmd.OutOrStdout(),"Successfully upgraded janus from v%s to v%s.\n",d.version,next);return nil
    }
    return cmd
}
func brewVersion(ctx context.Context,t target,outputCommand func(*exec.Cmd)([]byte,error))(string,error){
    output,err:=outputCommand(exec.CommandContext(ctx,t.brew,"info","--json=v2",t.formula));if err!=nil{return "",fmt.Errorf("Homebrew version check: %w",err)}
    var data struct{Formulae []struct{Versions struct{Stable string `json:"stable"`} `json:"versions"`} `json:"formulae"`}
    if json.Unmarshal(output,&data)!=nil||len(data.Formulae)!=1{return "",errors.New("invalid Formula metadata")}
    v:=data.Formulae[0].Versions.Stable;if _,err:=parse(v);err!=nil{return "",err};return v,nil
}
