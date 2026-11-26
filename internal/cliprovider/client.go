package cliprovider

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "time"
    "unicode"

    "github.com/leo1394/homebrew-janus/internal/store"
)

const origin="https://iam.clobotics.cn"
var ErrUnavailable=errors.New("CLI provider operation failed; no credentials or remote diagnostics returned")
type Principal struct{ID string `json:"id"`;TenantID string `json:"tenantID"`;Username string `json:"username"`;DisplayName string `json:"displayName"`}
type Identity struct{Authenticated bool `json:"authenticated"`;Environment string `json:"environment"`;System string `json:"system"`;Principal Principal `json:"principal"`}
type tokens struct{Access string `json:"accessToken"`;Refresh string `json:"refreshToken"`;Type string `json:"tokenType"`;Expires int64 `json:"expiresIn"`;RefreshExpires int64 `json:"refreshExpiresIn"`;ID string `json:"userId"`;Tenant string `json:"tenantId"`}
type Client struct{account store.Account;http *http.Client;token tokens;expires time.Time}
func New(a store.Account)*Client{return &Client{account:a,http:&http.Client{Timeout:30*time.Second,Transport:&http.Transport{},CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}}}
func safe(s string,max int,empty bool)bool{if len(s)>max||(!empty&&s==""){return false};for _,r:=range s{if unicode.IsControl(r){return false}};return true}
func(c *Client)request(ctx context.Context,path string,body any,out any)error{
    method:="GET";var data []byte;var err error;if body!=nil{method="POST";data,err=json.Marshal(body);if err!=nil{return ErrUnavailable};defer func(){for i:=range data{data[i]=0}}()}
    request,err:=http.NewRequestWithContext(ctx,method,origin+"/api/v1/auth/"+path,bytes.NewReader(data));if err!=nil{return ErrUnavailable}
    request.Header.Set("Accept","application/json");request.Header.Set("Content-Type","application/json");request.Header.Set("X-Clobotics-Environment",c.account.Environment)
    if path!="login"&&c.token.Access!=""{request.Header.Set("Authorization","Bearer "+c.token.Access);request.Header.Set("X-Clobotics-User-ID",c.token.ID);if c.token.Tenant!=""{request.Header.Set("X-Clobotics-Tenant-ID",c.token.Tenant)}}
    response,err:=c.http.Do(request);if err!=nil{return ErrUnavailable};defer response.Body.Close()
    data,err=io.ReadAll(io.LimitReader(response.Body,65537));if err!=nil||len(data)>65536||response.StatusCode!=200{return ErrUnavailable}
    var envelope struct{Code *int `json:"code"`;Data json.RawMessage `json:"data"`};if json.Unmarshal(data,&envelope)!=nil||envelope.Code==nil||*envelope.Code!=0{return ErrUnavailable}
    if out!=nil&&json.Unmarshal(envelope.Data,out)!=nil{return ErrUnavailable};return nil
}
func(c *Client)Login(ctx context.Context,secret []byte)error{
    if c.account.Provider!="clobotics"||c.account.Host!="iam.clobotics.cn"||c.account.Port!=443||!store.ValidCLIEnvironment(c.account.Environment){return ErrUnavailable}
    var token tokens;if err:=c.request(ctx,"login",map[string]string{"username":c.account.User,"password":string(secret)},&token);err!=nil{return err}
    if !safe(token.Access,8192,false)||!safe(token.Refresh,8192,false)||!safe(token.ID,256,false)||!safe(token.Tenant,256,true)||token.Expires<=0||(token.Type!=""&&token.Type!="Bearer"&&token.Type!="bearer"){return ErrUnavailable}
    c.token=token;c.expires=time.Now().Add(time.Duration(min(token.Expires,86400))*time.Second)
    _,err:=c.Inspect(ctx);return err
}
func(c *Client)Inspect(ctx context.Context)(Identity,error){
    if c.token.Access==""||!time.Now().Before(c.expires){return Identity{},ErrUnavailable}
    var me struct{ID string `json:"id"`;UserID string `json:"userId"`;Tenant string `json:"tenantId"`;Username string `json:"username"`;Display string `json:"displayName"`}
    if err:=c.request(ctx,"me",nil,&me);err!=nil{return Identity{},err};id:=me.UserID;if id==""{id=me.ID};tenant:=me.Tenant;if tenant==""{tenant=c.token.Tenant}
    if id!=c.token.ID||(me.ID!=""&&me.ID!=id)||me.Username!=c.account.User||(c.token.Tenant!=""&&tenant!=c.token.Tenant)||!safe(me.Display,256,true)||!safe(tenant,256,true){return Identity{},ErrUnavailable}
    return Identity{true,c.account.Environment,"rea",Principal{id,tenant,me.Username,me.Display}},nil
}
func(c *Client)Close(){ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel();if c.token.Access!=""{_ = c.request(ctx,"logout",map[string]string{},nil)};c.token=tokens{};c.http.CloseIdleConnections()}
