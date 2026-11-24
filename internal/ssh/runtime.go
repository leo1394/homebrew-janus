// Adapted from ConfidentialSSHOwnedMaster / session attachment: one persistent
// OpenSSH master, password GUI only at master creation, fail-closed attachments.
package ssh

import (
    "context"
    "crypto/sha256"
    "errors"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/leo1394/homebrew-janus/internal/store"
    "github.com/leo1394/homebrew-janus/internal/timing"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

type Runtime struct {
    Store store.Store
    Binary string
    Helper string
    CredentialNamespace string
    AgentControlSocket string
    In io.Reader
    Out, Err io.Writer
    // Run is an injectable process seam. Production always uses /usr/bin/ssh.
    Run func(context.Context, []string, []string, bool) error
}

func (r *Runtime) path(p janus.Profile) string {
    sum := sha256.Sum256([]byte(p.ID))
    return filepath.Join(r.Store.Root, "ssh", fmt.Sprintf("%x.sock", sum[:12]))
}
func (r *Runtime) base(p janus.Profile) []string {
    return []string{"-F", "/dev/null", "-S", r.path(p), "-p", strconv.Itoa(p.Port), "-l", p.User, "-o", "StrictHostKeyChecking=ask", "-o", "ConnectTimeout=20", "-o", "ServerAliveInterval=30", "-o", "ServerAliveCountMax=3"}
}
func (r *Runtime) run(ctx context.Context, args, env []string, terminal bool) error {
    if r.Run != nil { return r.Run(ctx, args, env, terminal) }
    binary := r.Binary; if binary == "" { binary = "/usr/bin/ssh" }
    // The CLI hands its original file descriptors and process directly to SSH.
    // No Janus tracing, stream copying or exit handling remains in the session.
    if terminal && r.In == os.Stdin && r.Out == os.Stdout && r.Err == os.Stderr {
        if err := ctx.Err(); err != nil { return err }
        return syscall.Exec(binary,append([]string{binary},args...),os.Environ())
    }
    failed := true
    if !terminal && os.Getenv("JANUS_TRACE")=="1" {
        path,stop,err:=traceSSH(r.Err);if err!=nil{return err};defer func(){stop(failed)}()
        args=append([]string{"-vvv","-E",path},args...)
    }
    cmd := exec.CommandContext(ctx, binary, args...)
    cmd.Env = os.Environ()
    if env != nil {
        // Do not inherit arbitrary askpass settings or credential context.
        filtered := []string{}
        for _, value := range cmd.Env {
            if strings.HasPrefix(value,"SSH_ASKPASS=") || strings.HasPrefix(value,"SSH_ASKPASS_REQUIRE=") || strings.HasPrefix(value,"JANUS_SSH_") || strings.HasPrefix(value,"LC_ALL=") || strings.HasPrefix(value,"DISPLAY=") || strings.HasPrefix(value,"JANUS_TRACE_EPOCH=") { continue }
            filtered = append(filtered, value)
        }
        cmd.Env = append(filtered, env...)
    }
    if terminal { cmd.Stdin = r.In; cmd.Stdout = r.Out; cmd.Stderr = r.Err } else { cmd.Stdin = nil; cmd.Stdout = io.Discard; cmd.Stderr = r.Err }
    timing.Mark("ssh_process_start")
    if err := cmd.Start(); err != nil { return err }
    timing.Mark("ssh_process_started")
    err := cmd.Wait()
    failed = err != nil
    if !terminal { timing.Mark("ssh_process_exited") }
    if err != nil && terminal { return fmt.Errorf("SSH session failed: %w; a local master check does not confirm remote responsiveness",err) }
    return err
}
func (r *Runtime) Connected(ctx context.Context, p janus.Profile) (bool, error) {
    if err := store.Validate(p); err != nil { return false, err }
    info, err := os.Lstat(r.path(p))
    if errors.Is(err, os.ErrNotExist) { return false, nil }
    if err != nil { return false, err }
    if info.Mode()&os.ModeSocket == 0 { return false, errors.New("SSH control path is not a socket; refusing reuse") }
    check, cancel := context.WithTimeout(ctx, time.Second); defer cancel()
    args := append(r.base(p), "-O", "check", p.Host)
    // Status must not write OpenSSH's master PID message into devices output.
    if r.Run == nil {
        binary := r.Binary; if binary == "" { binary = "/usr/bin/ssh" }
        cmd := exec.CommandContext(check, binary, args...)
        if err := cmd.Run(); err != nil { if check.Err() != nil { return false, check.Err() }; return false, nil }
        return true, nil
    }
    return r.run(check,args,nil,false) == nil, nil
}
func (r *Runtime) helper() (string, error) {
    if r.Helper != "" { return r.Helper, nil }
    binary, err := os.Executable(); if err != nil { return "", err }
    binary, err = filepath.EvalSymlinks(binary); if err != nil { return "", err }
    helper := filepath.Join(filepath.Dir(binary), "..", "libexec", "janus-keychain")
    info, err := os.Stat(helper)
    if err != nil || info.Mode().Perm()&0111 == 0 { return "", errors.New("Janus Confidential CLI Keychain helper is missing; run make build or reinstall Janus Confidential CLI") }
    return helper, nil
}
func (r *Runtime) Ensure(ctx context.Context, p janus.Profile) error { return r.ensure(ctx,p,nil) }
func (r *Runtime) ensure(ctx context.Context, p janus.Profile, options []string) error {return r.ensureRegistration(ctx,p,options,false)}
func (r *Runtime) ensureRegistration(ctx context.Context,p janus.Profile,options []string,pending bool)error{
    if err := store.Validate(p); err != nil { return err }
    timing.Mark("runtime_lock_start")
    unlock, err := r.Store.Lock(strings.TrimPrefix(strings.TrimPrefix(p.ID,"terminal:"),"agent:")); if err != nil { return err }; defer unlock()
    timing.Mark("runtime_lock_acquired")
    if pending {
        if !p.IsAgent()||p.ID==""{return errors.New("invalid pending agent identity")}
        rows,err:=r.Store.List();if err!=nil{return err};for _,row:=range rows{if row.ID==p.ID{return errors.New("pending agent is already registered")}}
    } else {
    current, err := r.Store.Get(p.ID); if err != nil { return err }
    if store.Target(current) != store.Target(p) || current.PersistSeconds != p.PersistSeconds || current.Platform!=p.Platform || current.Type!=p.Type || current.AgentTransport!=p.AgentTransport || current.InstanceName!=p.InstanceName || current.CredentialSlot!=p.CredentialSlot { return errors.New("device transport changed; select the device again") }
    }
    if runtime.GOOS != "darwin" && r.Run == nil { return errors.New("SSH credential authorization requires macOS") }
    if err := store.SecureDir(filepath.Join(r.Store.Root,"ssh")); err != nil { return err }
    if len(r.path(p)) > 100 { return errors.New("Janus Confidential CLI home path is too long for an SSH control socket") }
    timing.Mark("master_check_start")
    live, err := r.Connected(ctx,p); if err != nil { return err }; if live { timing.Mark("master_reused"); return nil }
    timing.Mark("master_check_complete")
    if _, err := os.Lstat(r.path(p)); err == nil { return errors.New("stale SSH control socket; run disconnect before reconnecting") }
    helper, err := r.helper(); if err != nil { return err }
    if os.Getenv("JANUS_TRACE")=="1" { fmt.Fprintf(os.Stderr,"janus helper_path=%s\n",filepath.Clean(helper)) }
    env := []string{"JANUS_TRACE_EPOCH="+timing.Epoch(),"SSH_ASKPASS="+helper,"SSH_ASKPASS_REQUIRE=force","DISPLAY=janus","LC_ALL=C","JANUS_SSH_HOST="+p.Host,"JANUS_SSH_USER="+p.User,"JANUS_SSH_PORT="+strconv.Itoa(p.Port),"JANUS_SSH_NAMESPACE="+r.CredentialNamespace,"JANUS_SSH_RECOVERY_SLOT="+p.CredentialSlot}
    masterBase := r.base(p)
    if options != nil { masterBase = []string{"-S",r.path(p),"-p",strconv.Itoa(p.Port),"-l",p.User,"-o","HostName="+p.Host}; masterBase = append(masterBase,options...) }
    args := append(masterBase, "-M", "-N", "-f", "-o", "ControlPersist="+strconv.Itoa(p.PersistSeconds), "-o", "PreferredAuthentications=password", "-o", "PubkeyAuthentication=no", "-o", "NumberOfPasswordPrompts=1", p.Host)
    started := time.Now()
    trace("transport_start",started)
    if err := r.run(ctx,args,env,false); err != nil { return fmt.Errorf("SSH master creation failed: %w",err) }
    live, err = r.Connected(ctx,p)
    if err != nil { return err }; if !live { return errors.New("SSH master did not become ready") }
    trace("master_ready",started)
    return nil
}
func (r *Runtime) Disconnect(ctx context.Context, p janus.Profile) error {
    if err := store.Validate(p); err != nil { return err }
    unlock, err := r.Store.Lock(strings.TrimPrefix(strings.TrimPrefix(p.ID,"terminal:"),"agent:")); if err != nil { return err }; defer unlock()
    live, err := r.Connected(ctx,p); if err != nil { return err }
    if !live {
        if info, err := os.Lstat(r.path(p)); err == nil && info.Mode()&os.ModeSocket != 0 { return os.Remove(r.path(p)) }
        return nil
    }
    // OpenSSH mux stop-listening (not exit): existing channels drain normally.
    if err := r.run(ctx,append(r.base(p),"-O","stop",p.Host),nil,false); err != nil { return fmt.Errorf("stop SSH reuse: %w",err) }
    return nil
}
func (r *Runtime) Attach(ctx context.Context, p janus.Profile) error {
    timing.Mark("session_attach_start")
    mode := "-T"
    if file, ok := r.In.(*os.File); ok { info, _ := file.Stat(); if info != nil && info.Mode()&os.ModeCharDevice != 0 { mode = "-tt" } }
    args := append(r.base(p), "-o", "ControlMaster=no", "-o", "BatchMode=yes", "-o", "ProxyCommand=/usr/bin/false", mode, p.Host)
    return r.run(ctx,args,nil,true)
}
func (r *Runtime) DeleteCredential(ctx context.Context, p janus.Profile) error {
    helper, err := r.helper(); if err != nil { return err }
    cmd := exec.CommandContext(ctx,helper,"--delete",p.Host,p.User,strconv.Itoa(p.Port))
    cmd.Stderr = r.Err
    for _,value:=range os.Environ(){if !strings.HasPrefix(value,"JANUS_SSH_NAMESPACE=")&&!strings.HasPrefix(value,"JANUS_SSH_RECOVERY_SLOT="){cmd.Env=append(cmd.Env,value)}}
    cmd.Env=append(cmd.Env,"JANUS_SSH_NAMESPACE="+r.CredentialNamespace,"JANUS_SSH_RECOVERY_SLOT="+p.CredentialSlot)
    if err := cmd.Run(); err != nil { return fmt.Errorf("Keychain deletion failed; device retained: %w",err) }
    return nil
}
func trace(phase string, start time.Time) {
    if os.Getenv("JANUS_TRACE") == "1" { fmt.Fprintf(os.Stderr,"janus phase=%s elapsed_ms=%d operation_ms=%d\n",phase,time.Since(timing.Start).Milliseconds(),time.Since(start).Milliseconds()) }
}
