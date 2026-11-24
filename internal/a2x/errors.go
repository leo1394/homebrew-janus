package a2x

import (
    "errors"
    "regexp"
    "strconv"
)
var a2aCodePattern=regexp.MustCompile(`^A2A (-[0-9]{5})(?: |$)`)
// Preserve machine-readable protocol codes, never remote error messages/data:
// a remote exception may contain a credential, user prompt or internal path.
func SafeErrorCode(err error)int{
    var remote *rpcError
    if !errors.As(err,&remote){return -32603}
    code:=remote.Code
    if match:=a2aCodePattern.FindStringSubmatch(remote.Message);match!=nil{code,_=strconv.Atoi(match[1])}
    if code>=-32099&&code<=-32000{return code}
    switch code{case -32700,-32600,-32601,-32602,-32603:return code}
    return -32603
}
