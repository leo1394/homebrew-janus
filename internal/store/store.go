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
    "strings"
    "syscall"
    "golang.org/x/sys/unix"

    "github.com/leo1394/homebrew-janus/pkg/janus"
)

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:-]{0,252}$`)
var userPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)
var idPattern = regexp.MustCompile(`^terminal:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Store struct { Root string }
type document struct {
    Schema int `json:"schemaVersion"`
    Profiles []janus.Profile `json:"profiles"`
}

func Default() (Store, error) {
    home, err := os.UserHomeDir()
    if err != nil { return Store{}, err }
    return Store{Root: filepath.Join(home, ".janus", "base")}, nil
}

func Validate(p janus.Profile) error {
    if !hostPattern.MatchString(p.Host) || !userPattern.MatchString(p.User) || p.Port < 1 || p.Port > 65535 { return errors.New("invalid SSH user, host or port") }
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

func (s Store) List() ([]janus.Profile, error) {
    path := filepath.Join(s.Root, "devices.json")
    fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|unix.O_CLOEXEC, 0)
    if errors.Is(err, os.ErrNotExist) { return []janus.Profile{}, nil }
    if err != nil { return nil, err }
    f := os.NewFile(uintptr(fd), path); defer f.Close()
    info, err := f.Stat()
    if err != nil { return nil, err }
    if !info.Mode().IsRegular() || info.Size() > 2<<20 || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) { return nil, errors.New("unsafe device registry") }
    var doc document
    dec := json.NewDecoder(io.LimitReader(f, 2<<20)); dec.DisallowUnknownFields()
    if err := dec.Decode(&doc); err != nil { return nil, fmt.Errorf("invalid device registry: %w", err) }
    var extra any
    if err := dec.Decode(&extra); err != io.EOF { return nil, errors.New("trailing registry content") }
    if doc.Schema != 1 || len(doc.Profiles) > 2000 { return nil, errors.New("unsupported device registry") }
    ids, names, targets := map[string]bool{}, map[string]bool{}, map[string]bool{}
    for _, p := range doc.Profiles {
        if err := Validate(p); err != nil { return nil, err }
        if p.ID == "" || ids[p.ID] || names[strings.ToLower(p.Name)] || targets[Target(p)] { return nil, errors.New("duplicate or empty device identity") }
        ids[p.ID] = true; names[strings.ToLower(p.Name)] = true; targets[Target(p)] = true
    }
    if doc.Profiles == nil { doc.Profiles = []janus.Profile{} }
    return doc.Profiles, nil
}

func Target(p janus.Profile) string { return fmt.Sprintf("%s@%s:%d", p.User, strings.ToLower(p.Host), p.Port) }

func (s Store) Save(profiles []janus.Profile) error {
    if err := SecureDir(s.Root); err != nil { return err }
    data, err := json.MarshalIndent(document{Schema: 1, Profiles: profiles}, "", "  ")
    if err != nil { return err }
    tmp, err := os.CreateTemp(s.Root, ".devices-")
    if err != nil { return err }
    defer os.Remove(tmp.Name())
    if _, err = tmp.Write(append(data, '\n')); err == nil { err = tmp.Sync() }
    closeErr := tmp.Close(); if err != nil { return err }; if closeErr != nil { return closeErr }
    if err := os.Rename(tmp.Name(), filepath.Join(s.Root, "devices.json")); err != nil { return err }
    dir, err := os.Open(s.Root); if err != nil { return err }; defer dir.Close(); return dir.Sync()
}

func (s Store) Add(user, host string, port int) (janus.Profile, bool, error) {
    unlock, err := s.Lock("registry"); if err != nil { return janus.Profile{}, false, err }; defer unlock()
    rows, err := s.List(); if err != nil { return janus.Profile{}, false, err }
    p := janus.Profile{Host: strings.ToLower(host), User: user, Port: port, Name: user+"@"+strings.ToLower(host), PersistSeconds: 7200}
    if err := Validate(p); err != nil { return p, false, err }
    names := map[string]bool{}
    for _, existing := range rows {
        if Target(existing) == Target(p) { return existing, false, nil }
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
        if err := deleteCredential(p); err != nil { return err }
        if err := s.Save(append(rows[:i], rows[i+1:]...)); err != nil { return fmt.Errorf("credential deleted but device record could not be removed; retry remove: %w", err) }
        return nil
    }
    return errors.New("device no longer exists")
}
