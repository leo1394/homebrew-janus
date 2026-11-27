package upgrade

import (
    "archive/tar"
    "bytes"
    "compress/gzip"
    "context"
    "crypto/sha256"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "time"

    "github.com/leo1394/homebrew-janus/internal/metadata"
)

func prepareRelease(ctx context.Context,next string)(string,func(),error){
    if _,err:=parse(next);err!=nil{return "",nil,err}
    if runtime.GOOS!="darwin"||(runtime.GOARCH!="arm64"&&runtime.GOARCH!="amd64"){return "",nil,fmt.Errorf("no precompiled release for %s/%s",runtime.GOOS,runtime.GOARCH)}
    arch:=runtime.GOARCH;if arch=="amd64"{arch="x86_64"}
    asset:="janus_"+next+"_darwin_"+arch+".tar.gz"
    base:=metadata.Repository+"/releases/download/v"+next+"/"
    client:=&http.Client{Timeout:120*time.Second,CheckRedirect:func(req *http.Request,via []*http.Request)error{
        if req.URL.Scheme!="https"||len(via)>=10{return fmt.Errorf("unsafe or excessive download redirects")};return nil
    }}
    checksums,err:=download(ctx,client,base+"SHA256SUMS",1<<20);if err!=nil{return "",nil,err}
    archive,err:=download(ctx,client,base+asset,128<<20);if err!=nil{return "",nil,err}
    if err:=verifyChecksum(archive,checksums,asset);err!=nil{return "",nil,err}
    directory,err:=os.MkdirTemp("","janus-upgrade-");if err!=nil{return "",nil,err}
    cleanup:=func(){_ = os.RemoveAll(directory)}
    if err:=unpackRelease(archive,directory);err!=nil{cleanup();return "",nil,err}
    return directory,cleanup,nil
}
func download(ctx context.Context,client *http.Client,url string,limit int64)([]byte,error){
    request,err:=http.NewRequestWithContext(ctx,http.MethodGet,url,nil);if err!=nil{return nil,err}
    if request.URL.Scheme!="https"{return nil,fmt.Errorf("release downloads require HTTPS")}
    response,err:=client.Do(request);if err!=nil{return nil,err};defer response.Body.Close()
    if response.StatusCode!=http.StatusOK{return nil,fmt.Errorf("release download failed: HTTP %d",response.StatusCode)}
    data,err:=io.ReadAll(io.LimitReader(response.Body,limit+1));if err!=nil{return nil,err}
    if int64(len(data))>limit{return nil,fmt.Errorf("release download exceeds size limit")};return data,nil
}
func verifyChecksum(data,manifest []byte,name string)error{
    expected:="";count:=0
    for _,line:=range strings.Split(string(manifest),"\n"){
        fields:=strings.Fields(line);if len(fields)==2&&strings.TrimPrefix(fields[1],"*")==name{expected=fields[0];count++}
    }
    if count!=1||expected!=fmt.Sprintf("%x",sha256.Sum256(data)){return fmt.Errorf("missing, duplicate or mismatched SHA256 for %s",name)};return nil
}
func unpackRelease(archive []byte,directory string)error{
    required:=map[string]bool{"bin/janus":true,"libexec/janus-keychain":true,"share/janus/install.sh":true,"SHA256SUMS":true}
    allowed:=map[string]bool{"bin/janus":true,"libexec/janus-keychain":true,"libexec/janus-keychain-migrate":true,"share/janus/install.sh":true,"SHA256SUMS":true}
    seen:=map[string]bool{}
    compressed,err:=gzip.NewReader(bytes.NewReader(archive));if err!=nil{return err};defer compressed.Close()
    reader:=tar.NewReader(compressed);var total int64
    for {
        header,err:=reader.Next();if err==io.EOF{break};if err!=nil{return err}
        if !allowed[header.Name]||seen[header.Name]||header.Typeflag!=tar.TypeReg{return fmt.Errorf("unsafe or unexpected release entry: %s",header.Name)}
        total+=header.Size;if header.Size<0||total>256<<20{return fmt.Errorf("release payload exceeds size limit")}
        path:=filepath.Join(directory,filepath.FromSlash(header.Name))
        if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil{return err}
        file,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0700);if err!=nil{return err}
        _,copyErr:=io.Copy(file,reader);closeErr:=file.Close();if copyErr!=nil{return copyErr};if closeErr!=nil{return closeErr}
        seen[header.Name]=true
    }
    for name:=range required{if !seen[name]{return fmt.Errorf("release payload missing %s",name)}}
    manifest,err:=os.ReadFile(filepath.Join(directory,"SHA256SUMS"));if err!=nil{return err}
    for name:=range seen{if name=="SHA256SUMS"{continue};data,err:=os.ReadFile(filepath.Join(directory,filepath.FromSlash(name)));if err!=nil{return err};if err:=verifyChecksum(data,manifest,name);err!=nil{return err}}
    return nil
}
