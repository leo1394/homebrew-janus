package cliprovider

import (
    "context"
    "io"
    "net/http"
    "strings"
    "testing"
    "time"

    "github.com/leo1394/homebrew-janus/internal/store"
)
type transport func(*http.Request)(*http.Response,error)
func(f transport)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
func fixtureAccount()store.Account{return store.Account{ID:"account:00000000000000000000000000000001",Provider:"clobotics",Host:"iam.clobotics.cn",Port:443,User:"fixture",Environment:"test",Revision:1}}
func TestIdentityBindingAndNoTokenOutput(t *testing.T){
    for _,mismatch:=range []bool{false,true}{
        c:=New(fixtureAccount());logins:=0
        c.http.Transport=transport(func(r *http.Request)(*http.Response,error){
            if r.URL.Scheme!="https"||r.URL.Host!="iam.clobotics.cn"||r.Header.Get("X-Clobotics-Environment")!="test"{t.Fatal("unbound destination")}
            body:=`{"code":0,"data":{}}`
            switch r.URL.Path{
            case "/api/v1/auth/login":logins++;body=`{"code":0,"data":{"accessToken":"private-access","refreshToken":"private-refresh","userId":"u1","expiresIn":600,"refreshExpiresIn":3600}}`
            case "/api/v1/auth/me":if r.Header.Get("Authorization")!="Bearer private-access"{t.Fatal("missing authorization")};name:="fixture";if mismatch{name="different"};body=`{"code":0,"data":{"id":"u1","username":"`+name+`","displayName":"Fixture"}}`
            }
            return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(body)),Header:make(http.Header)},nil
        })
        err:=c.Login(context.Background(),[]byte("synthetic"));if mismatch{if err==nil{t.Fatal("identity mismatch accepted")};c.Close();continue};if err!=nil{t.Fatal(err)}
        value,err:=c.Inspect(context.Background());if err!=nil||value.Principal.Username!="fixture"||logins!=1{t.Fatal("reuse failed",err)};c.Close();if c.token.Access!=""{t.Fatal("token retained after close")}
    }
}
func TestStrictRequest(t *testing.T){for _,line:=range []string{`{"version":1,"requestID":"a","method":"session.list","parameters":{},"extra":"x"}`,`{"version":2,"requestID":"a"}`,`{} {}`} {if _,err:=decode([]byte(line));err==nil{t.Fatal("invalid request accepted")}}}

func TestExpiredTokenCannotInspectOrRefresh(t *testing.T){
    c:=New(fixtureAccount());c.token=tokens{Access:"synthetic-access",Refresh:"synthetic-refresh"};c.expires=time.Now().Add(-time.Second)
    calls:=0;c.http.Transport=transport(func(r *http.Request)(*http.Response,error){calls++;return nil,ErrUnavailable})
    if _,err:=c.Inspect(context.Background());err==nil{t.Fatal("expired token accepted")}
    if calls!=0{t.Fatal("expired token sent or silently refreshed")}
}
