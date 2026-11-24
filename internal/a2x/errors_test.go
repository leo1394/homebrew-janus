package a2x

import (
    "errors"
    "testing"
)
func TestSafeA2AErrorCodes(t *testing.T){
    for _,tc:=range []struct{err error;code int}{{&rpcError{-32000,"A2A -32003 secret"},-32003},{&rpcError{-32002,"secret"},-32002},{&rpcError{-32000,"A2A -32602 secret"},-32602},{&rpcError{123,"A2A -99999 secret"},-32603},{errors.New("A2A -32003 untrusted local text"),-32603}}{if got:=SafeErrorCode(tc.err);got!=tc.code{t.Fatal(got,tc.code)}}
}
