package cli

import (
    "crypto/sha256"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/leo1394/homebrew-janus/internal/metadata"
    "github.com/leo1394/homebrew-janus/internal/store"
)
// Development isolation is opt-in and unavailable in a stable build. Resolve
// existing ancestors so aliases cannot accidentally select the production store.
func canonicalDataPath(path string)(string,error){
    absolute,err:=filepath.Abs(path);if err!=nil{return "",err}
    resolved,err:=filepath.EvalSymlinks(absolute);if err==nil{return resolved,nil}
    if !os.IsNotExist(err){return "",err}
    parent:=filepath.Dir(absolute);if parent==absolute{return "",err}
    resolved,err=canonicalDataPath(parent);if err!=nil{return "",err}
    return filepath.Join(resolved,filepath.Base(absolute)),nil
}
func developmentData(version,path,socket string)(store.Store,string,error){
    if path=="" {if socket!=""{return store.Store{},"",errors.New("JANUS_DEV_CONTROL_SOCKET requires JANUS_DEV_DATA_DIR")};return store.Store{},"",nil}
    if !strings.HasSuffix(version,"-dev"){return store.Store{},"",errors.New("development data directory requires a -dev build")}
    if !filepath.IsAbs(path){return store.Store{},"",errors.New("JANUS_DEV_DATA_DIR must be absolute")}
    root,err:=canonicalDataPath(path);if err!=nil{return store.Store{},"",err}
    production,err:=store.Default();if err!=nil{return store.Store{},"",err}
    productionPath,err:=canonicalDataPath(production.Root);if err!=nil{return store.Store{},"",err}
    relative,err:=filepath.Rel(filepath.Dir(productionPath),root);if err!=nil{return store.Store{},"",err}
    if relative=="."||relative!=".."&&!strings.HasPrefix(relative,".."+string(filepath.Separator)){return store.Store{},"",errors.New("development data must be outside the production Janus directory")}
    hash:=sha256.Sum256([]byte(root))
    return store.Store{Root:root},fmt.Sprintf("%x",hash),nil
}
func developmentEnvironment()(store.Store,string,string,error){
    socket:=os.Getenv("JANUS_DEV_CONTROL_SOCKET")
    s,scope,err:=developmentData(metadata.Version,os.Getenv("JANUS_DEV_DATA_DIR"),socket)
    return s,scope,socket,err
}
