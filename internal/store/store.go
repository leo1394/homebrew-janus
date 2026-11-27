// Profile identity and credential keys follow Janus Confidential's SSH registry.
package store

import (
    "crypto/rand"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strings"
    "syscall"
    "golang.org/x/sys/unix"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$`)
var userPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)
var idPattern = regexp.MustCompile(`^(terminal|agent|database):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Store struct { Root string }
type document struct {
    Schema int `json:"schemaVersion"`
    Profiles []janus.Profile `json:"profiles"`
    Accounts []Account `json:"accounts,omitempty"`
}

func Default() (Store, error) {
    home, err := os.UserHomeDir()
    if err != nil { return Store{}, err }
    return Store{Root: filepath.Join(home, ".janus", "base")}, nil
}

func Validate(p janus.Profile) error {
    if p.CredentialSlot!="" && !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(p.CredentialSlot) { return errors.New("invalid credential slot") }
    if !hostPattern.MatchString(p.Host) || !userPattern.MatchString(p.User) || p.Port < 1 || p.Port > 65535 { return errors.New("invalid SSH user, host or port") }
    if !p.IsAgent() && !p.IsMySQL() && (p.DevicePlatform()!="terminal" || p.DeviceType()!="ssh") { return errors.New("unsupported device platform/type") }
    if p.IsMySQL(){
        if p.DatabaseTLS!=""&&p.DatabaseTLS!="verify"&&p.DatabaseTLS!="disabled"{return errors.New("invalid database TLS mode")};if p.DatabaseTLS=="disabled"&&p.DatabaseCA!=""{return errors.New("CA cannot be used with disabled TLS")}
        if !accountIDPattern.MatchString(p.AccountID)||len(p.Database)>64||strings.ContainsAny(p.Database,"\x00\r\n")||len(p.DatabaseCA)>65536{return errors.New("invalid database account or target")}
    }else if p.AccountID!=""||p.Database!=""||p.DatabaseCA!=""||p.DatabaseTLS!=""{return errors.New("database metadata on non-database device")}
    if p.AgentTransport!="" && (!p.IsAgent() || (p.AgentTransport!="subsystem" && p.AgentTransport!="stdio")) { return errors.New("invalid agent transport") }
    if p.InstanceName!="" && (!p.IsAgent() || !janus.ValidAgentType(p.InstanceName)) { return errors.New("invalid named agent instance") }
    if len(p.InstanceID)>256 || strings.ContainsAny(p.InstanceID,"\x00\r\n\t\x1b") { return errors.New("invalid agent instance ID") }
    if len(p.LastSession)>1024 || strings.ContainsAny(p.LastSession,"\x00\r\n\x1b") { return errors.New("invalid agent session") }
    if p.ID != "" && !idPattern.MatchString(p.ID) { return errors.New("invalid device ID") }
    if strings.TrimSpace(p.Name) == "" || len(p.Name) > 800 || strings.ContainsAny(p.Name, "\x00\r\n\t\x1b") { return errors.New("invalid device name") }
    for _, r := range p.Name { if r < 32 || r == 127 { return errors.New("invalid device name") } }
    if p.PersistSeconds < 0 || p.PersistSeconds > 86400 { return errors.New("invalid master persistence") }
    return nil
}

func SecureDir(path string) error {
    // Check the immediate namespace owner before creating children: in the
    // default layout this rejects a redirected ~/.janus or base directory.
    parent, err := os.Lstat(filepath.Dir(path))
    if err == nil {
        stat, ok := parent.Sys().(*syscall.Stat_t)
        if !ok || !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 || (parent.Mode().Perm()&0022 != 0 && parent.Mode()&os.ModeSticky == 0) || (stat.Uid != uint32(os.Getuid()) && stat.Uid != 0) { return fmt.Errorf("unsafe Janus Confidential CLI parent directory: %s", filepath.Dir(path)) }
    } else if !errors.Is(err, os.ErrNotExist) { return err }
    // Require private, real directories; never chmod an existing symlink target.
    if err := os.MkdirAll(path, 0700); err != nil { return err }
    info, err := os.Lstat(path)
    if err != nil { return err }
    stat, ok := info.Sys().(*syscall.Stat_t)
    if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Getuid()) { return fmt.Errorf("unsafe Janus Confidential CLI directory: %s (must be owned by you with mode 0700)", path) }
    return nil
}

func (s Store) Lock(name string) (func(), error) {
    if err := SecureDir(s.Root); err != nil { return nil, err }
    if strings.ContainsAny(name, `/\\`) { return nil, errors.New("invalid lock name") }
    fd, err := syscall.Open(filepath.Join(s.Root, name+".lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
    if err != nil { return nil, err }
    file := os.NewFile(uintptr(fd), name)
    info, err := file.Stat()
    if err != nil { file.Close(); return nil, err }
    stat := info.Sys().(*syscall.Stat_t)
    if !info.Mode().IsRegular() || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0077 != 0 { file.Close(); return nil, errors.New("unsafe lock file") }
    if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil { file.Close(); return nil, err }
    return func() { syscall.Flock(fd, syscall.LOCK_UN); file.Close() }, nil
}

func (s Store) readDocument() (document, error) {
    path := filepath.Join(s.Root, "devices.json")
    fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|unix.O_CLOEXEC, 0)
    if errors.Is(err, os.ErrNotExist) { return document{Schema:1,Profiles:[]janus.Profile{}}, nil }
    if err != nil { return document{}, err }
    f := os.NewFile(uintptr(fd), path); defer f.Close()
    info, err := f.Stat()
    if err != nil { return document{}, err }
    if !info.Mode().IsRegular() || info.Size() > 2<<20 || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) { return document{}, errors.New("unsafe device registry") }
    var doc document
    dec := json.NewDecoder(io.LimitReader(f, 2<<20)); dec.DisallowUnknownFields()
    if err := dec.Decode(&doc); err != nil { return document{}, fmt.Errorf("invalid device registry: %w", err) }
    var extra any
    if err := dec.Decode(&extra); err != io.EOF { return document{}, errors.New("trailing registry content") }
    if doc.Schema != 1 || len(doc.Profiles) > 2000 { return document{}, errors.New("unsupported device registry") }
    normalizeAccounts(&doc)
    ids, names, targets := map[string]bool{}, map[string]bool{}, map[string]bool{}
    for _, p := range doc.Profiles {
        if err := Validate(p); err != nil { return document{}, err }
        if p.ID == "" || ids[p.ID] || names[strings.ToLower(p.Name)] || targets[deviceKey(p)] { return document{}, errors.New("duplicate or empty device identity") }
        ids[p.ID] = true; names[strings.ToLower(p.Name)] = true; targets[deviceKey(p)] = true
    }
    if doc.Profiles == nil { doc.Profiles = []janus.Profile{} }
    if err:=validateAccounts(doc);err!=nil{return document{},err}
    return doc, nil
}

func Target(p janus.Profile) string { return fmt.Sprintf("%s@%s:%d", p.User, strings.ToLower(p.Host), p.Port) }

func (s Store) List() ([]janus.Profile,error){
    doc,err:=s.readDocument();if err!=nil{return nil,err}
    sort.SliceStable(doc.Profiles,func(i,j int)bool{return janus.PlatformOrder(doc.Profiles[i].DevicePlatform())<janus.PlatformOrder(doc.Profiles[j].DevicePlatform())})
    return doc.Profiles,nil
}

func (s Store) Save(profiles []janus.Profile) error {
    doc,err:=s.readDocument();if err!=nil{return err};doc.Profiles=profiles
    return s.saveDocument(doc)
}
func(s Store)saveDocument(doc document)error{
    normalizeAccounts(&doc)
    if err:=validateAccounts(doc);err!=nil{return err}
    if err := SecureDir(s.Root); err != nil { return err }
    data, err := json.MarshalIndent(doc, "", "  ")
    if err != nil { return err }
    if len(data)+1>2<<20{return errors.New("device registry exceeds 2 MiB")}
    tmp, err := os.CreateTemp(s.Root, ".devices-")
    if err != nil { return err }
    defer os.Remove(tmp.Name())
    if _, err = tmp.Write(append(data, '\n')); err == nil { err = tmp.Sync() }
    closeErr := tmp.Close(); if err != nil { return err }; if closeErr != nil { return closeErr }
    if err := os.Rename(tmp.Name(), filepath.Join(s.Root, "devices.json")); err != nil { return err }
    dir, err := os.Open(s.Root); if err != nil { return err }; defer dir.Close(); return dir.Sync()
}

func deviceKey(p janus.Profile) string { if p.IsMySQL(){return "database:mysql:"+Target(p)+":"+p.Database+":"+p.AccountID};return p.DevicePlatform()+":"+p.DeviceType()+":"+Target(p)+":"+p.InstanceName }

func (s Store) Add(user, host string, port int) (janus.Profile, bool, error) { return s.add(user,host,port,"","") }
func (s Store) AddAgent(user, host string, port int, transport string) (janus.Profile, bool, error) {
    return s.AddAgentType(user,host,port,transport,"pi")
}
func (s Store) AddAgentType(user,host string,port int,transport,kind string)(janus.Profile,bool,error){
    if !janus.ValidAgentType(kind){return janus.Profile{},false,errors.New("agent type must match [a-z][a-z0-9-]{0,31}")}
    if transport==""{transport="subsystem"};return s.add(user,host,port,transport,kind)
}
func (s Store) add(user, host string, port int, transport,kind string) (janus.Profile, bool, error) {
    unlock, err := s.Lock("registry"); if err != nil { return janus.Profile{}, false, err }; defer unlock()
    rows, err := s.List(); if err != nil { return janus.Profile{}, false, err }
    p := janus.Profile{Host: strings.ToLower(host), User: user, Port: port, Name: user+"@"+strings.ToLower(host), PersistSeconds: 7200}
    if transport!="" {p.Platform="a2x";p.Type=kind;p.AgentTransport=transport}
    if err := Validate(p); err != nil { return p, false, err }
    names := map[string]bool{}
    for i, existing := range rows {
        if !existing.IsMySQL() && Target(existing)==Target(p){p.CredentialSlot=existing.CredentialSlot}
        if deviceKey(existing) == deviceKey(p) {
            if existing.IsAgent() && (existing.AgentTransport!=transport || existing.Platform!="a2x" || existing.Type!=kind) {existing.AgentTransport=transport;existing.Platform="a2x";existing.Type=kind;rows[i]=existing;return existing,false,s.Save(rows)}
            return existing, false, nil
        }
        names[strings.ToLower(existing.Name)] = true
    }
    if len(rows) >= 2000 { return p, false, errors.New("device limit reached") }
    if names[strings.ToLower(p.Name)] { p.Name += fmt.Sprintf(":%d", port) }
    base := p.Name
    for n := 2; names[strings.ToLower(p.Name)]; n++ { p.Name = fmt.Sprintf("%s (%d)", base, n) }
    var uuid [16]byte
    if _, err := rand.Read(uuid[:]); err != nil { return p, false, err }
    uuid[6] = (uuid[6]&15)|64; uuid[8] = (uuid[8]&63)|128
    p.ID = fmt.Sprintf("terminal:%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
    if p.IsAgent() { p.ID=strings.Replace(p.ID,"terminal:","agent:",1) }
    return p, true, s.Save(append(rows, p))
}

func (s Store) Get(id string) (janus.Profile, error) {
    rows, err := s.List(); if err != nil { return janus.Profile{}, err }
    for _, p := range rows { if p.ID == id { return p, nil } }
    return janus.Profile{}, errors.New("device was removed; refresh the selector")
}

func (s Store) Rename(id, name string) error {
    unlock, err := s.Lock("registry"); if err != nil { return err }; defer unlock()
    rows, err := s.List(); if err != nil { return err }
    found := false
    for i, p := range rows {
        if p.ID != id && strings.EqualFold(p.Name, name) { return fmt.Errorf("device name already exists: %s", name) }
        if p.ID == id { p.Name = name; if err := Validate(p); err != nil { return err }; rows[i] = p; found = true }
    }
    if !found { return errors.New("device no longer exists") }
    return s.Save(rows)
}

// Remove holds the registry lock through credential deletion. A failed deletion
// leaves the profile for retry. It deliberately never invokes runtime disconnect.
func (s Store) Remove(id string, deleteCredential func(janus.Profile) error) error {
    unlock, err := s.Lock("registry"); if err != nil { return err }; defer unlock()
    rows, err := s.List(); if err != nil { return err }
    for i, p := range rows {
        if p.ID != id { continue }
        shared:=false
        for _,other:=range rows{if other.ID!=p.ID && sameCredential(other,p){shared=true;break}}
        if !shared && !p.IsMySQL() {if err := deleteCredential(p); err != nil { return err }}
        if err := s.Save(append(rows[:i], rows[i+1:]...)); err != nil { return fmt.Errorf("credential deleted but device record could not be removed; retry remove: %w", err) }
        return nil
    }
    return errors.New("device no longer exists")
}

func (s Store) SetSession(id, session string) error {
    unlock,err:=s.Lock("registry");if err!=nil{return err};defer unlock()
    rows,err:=s.List();if err!=nil{return err}
    for i,p:=range rows {if p.ID==id {if !p.IsAgent(){return errors.New("not an agent device")};p.LastSession=session;if err:=Validate(p);err!=nil{return err};rows[i]=p;return s.Save(rows)}}
    return errors.New("device no longer exists")
}

// Bind only after authenticated agent.info. A changed server identity requires
// explicit owner intervention rather than silently moving existing delegations.
func (s Store) BindAgentInstance(id,instance string)error{return s.BindAgentIdentity(id,instance,"")}
func (s Store) BindAgentIdentity(id,instance,kind string)error{
    if kind!=""&&!janus.ValidAgentType(kind){return errors.New("unsupported agent.info.type")}
    if instance==""{return errors.New("agent.info did not provide instanceId")}
    unlock,err:=s.Lock("registry");if err!=nil{return err};defer unlock()
    rows,err:=s.List();if err!=nil{return err}
    for i,p:=range rows{if p.ID==id{
        if !p.IsAgent(){return errors.New("not an Agenux device")}
        if p.InstanceID!=""&&p.InstanceID!=instance{return errors.New("Agenux instance identity changed; owner review required")}
        p.Type=p.DeviceType();if kind!=""{p.Type=kind};p.Platform="a2x";p.InstanceID=instance
        for _,other:=range rows{if other.ID!=p.ID&&deviceKey(other)==deviceKey(p){return errors.New("agent type migration conflicts with another device")}}
        if err:=Validate(p);err!=nil{return err};rows[i]=p;return s.Save(rows)
    }}
    return errors.New("device no longer exists")
}
