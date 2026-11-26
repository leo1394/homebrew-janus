package ssh

import (
    "bytes"
    "context"
    "errors"
    "io"
    "os"
    "os/exec"
    "strings"
    "syscall"
    "time"

    "github.com/leo1394/homebrew-janus/internal/deviceprofile"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

// Fixed read-only probes, no environment dump, file search or user shell input.
const profileProbe = `export LC_ALL=C
printf 'system\t'; uname -s
printf 'architecture\t'; uname -m
printf 'kernel\t'; uname -r
printf 'user\t'; id -un
printf 'shell\t%s\n' "$SHELL"
case "$(uname -s)" in
 Darwin)
 printf 'os_version\t'; sw_vers -productVersion
 printf 'cpu\t'; sysctl -n machdep.cpu.brand_string
 printf 'cores\t'; sysctl -n hw.logicalcpu
 printf 'memory_bytes\t'; sysctl -n hw.memsize
 printf 'memory_snapshot\t'; vm_stat | tr '\n' ';'; printf '\n'
 printf 'gpu\t'; system_profiler SPDisplaysDataType 2>/dev/null | awk '/Chipset Model:|VRAM/{printf "%s;",$0} END{print ""}'
 ;;
 Linux)
 printf 'os_version\t'; awk -F= '/^PRETTY_NAME=/{print substr($0,index($0,"=")+1)}' /etc/os-release
 printf 'cpu\t'; awk -F: '/model name/{print $2;exit}' /proc/cpuinfo
 printf 'cores\t'; getconf _NPROCESSORS_ONLN
 printf 'memory_snapshot\t'; awk '/^MemTotal:|^MemAvailable:/{printf "%s %s %s;",$1,$2,$3} END{print ""}' /proc/meminfo
 printf 'gpu\t'; if command -v nvidia-smi >/dev/null 2>&1; then nvidia-smi --query-gpu=name,memory.total --format=csv,noheader 2>/dev/null | tr '\n' ';'; elif command -v lspci >/dev/null 2>&1; then lspci 2>/dev/null | awk '/VGA|3D controller|Display controller/{printf "%s;",$0}'; fi; printf '\n'
 ;;
esac
printf 'storage_kib\t'; df -Pk -l 2>/dev/null | tr '\n' ';'; printf '\n'
for runtime in python3 node docker; do
 if command -v "$runtime" >/dev/null 2>&1; then printf 'runtime_%s\t' "$runtime"; "$runtime" --version 2>/dev/null | head -n 1; fi
done
`
type profileOutput struct{bytes.Buffer}
func(b *profileOutput)Write(p []byte)(int,error){if b.Len()+len(p)>128<<10{return 0,errors.New("profile output limit")};return b.Buffer.Write(p)}

// CollectProfile cannot open a new transport or request credentials.
func(r *Runtime)CollectProfile(ctx context.Context,p janus.Profile)error{
    request,cancel:=context.WithTimeout(ctx,12*time.Second);defer cancel()
    binary:=r.Binary;if binary==""{binary="/usr/bin/ssh"}
    args:=append(r.base(p),"-o","ControlMaster=no","-o","BatchMode=yes","-o","ProxyCommand=/usr/bin/false","-T",p.Host,"sh -s")
    cmd:=exec.CommandContext(request,binary,args...);cmd.Stdin=strings.NewReader(profileProbe);var output profileOutput;cmd.Stdout=&output;cmd.Stderr=io.Discard
    section:=deviceprofile.Failure("ssh.read-only-probe","collection failed or timed out; no new connection attempted")
    resources:=section
    runErr:=cmd.Run()
    if runErr==nil{
        values:=map[string]string{};for _,line:=range strings.Split(output.String(),"\n"){key,value,ok:=strings.Cut(line,"\t");if ok{switch key{case "system","architecture","kernel","user","shell","os_version","cpu","cores","memory_bytes","memory_snapshot","gpu","storage_kib","runtime_python3","runtime_node","runtime_docker":values[key]=deviceprofile.SafeText(strings.TrimSpace(value))}}}
        status:="ok";for _,key:=range []string{"system","cpu","memory_snapshot","gpu","storage_kib"}{if values[key]==""{values[key]="unknown";status="partial"}}
        dynamic:=map[string]string{};for _,key:=range []string{"memory_snapshot","storage_kib"}{dynamic[key]=values[key];delete(values,key)}
        section=deviceprofile.Observation("ssh.read-only-probe",status,values,24*time.Hour)
        resources=deviceprofile.Observation("ssh.read-only-probe",status,dynamic,5*time.Minute)
    }
    if err:=deviceprofile.Merge(r.Store,p,map[string]deviceprofile.Section{"overview":section,"resources":resources});err!=nil{return err};if runErr!=nil{return errors.New(section.Error)};return nil
}
// SSH replaces the CLI process for terminal handoff. A bounded child owns the
// separate metadata channel; its output never reaches the user's terminal.
func(r *Runtime)startProfile(p janus.Profile){
    if r.Run!=nil||p.IsAgent()||p.IsMySQL()||deviceprofile.Fresh(r.Store,p,"resources"){return}
    executable,err:=os.Executable();if err!=nil{return}
    cmd:=exec.Command(executable,"__profile-ssh",p.ID);cmd.SysProcAttr=&syscall.SysProcAttr{Setsid:true}
    if err:=cmd.Start();err==nil{go cmd.Wait()}
}
