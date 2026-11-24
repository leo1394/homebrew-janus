package agentservice

import (
    "encoding/json"
    "regexp"
    "math"
    "unicode"
    "strings"
    "slices"
    "unicode/utf8"
)

var dispatchIDPattern=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func integerIn(raw json.RawMessage, low, high int64)bool{
    _,ok:=boundedInteger(raw,low,high)
    return ok
}

func boundedInteger(raw json.RawMessage, low, high int64)(float64,bool){
    var value *float64
    if json.Unmarshal(raw,&value)!=nil || value==nil{return 0,false}
    return *value,math.Trunc(*value)==*value && *value>=float64(low) && *value<=float64(high)
}

func validDispatch(raw json.RawMessage)bool{
    var fields map[string]json.RawMessage
    if json.Unmarshal(raw,&fields)!=nil{return false}
    if item,ok:=fields["workItemId"];ok{
        var id string
        if json.Unmarshal(item,&id)!=nil || !dispatchIDPattern.MatchString(id){return false}
        delete(fields,"workItemId")
    }
    if len(fields)!=5{return false}
    for _,name:=range []string{"dispatchId","workPackageId"}{
        var id string
        if json.Unmarshal(fields[name],&id)!=nil || !dispatchIDPattern.MatchString(id){return false}
    }
    var basis string
    if json.Unmarshal(fields["estimateBasis"],&basis)!=nil || strings.TrimFunc(basis,ecmaSpace)=="" || strings.ContainsRune(basis,0) || utf8.RuneCountInString(basis)>512{return false}
    if !integerIn(fields["nodeRevision"],1,2147483647){return false}
    estimate:=fields["estimatedMinutes"]
    if integerIn(estimate,1,525600){return true}
    var bounds map[string]json.RawMessage
    if json.Unmarshal(estimate,&bounds)!=nil || len(bounds)!=2{return false}
    min,okMin:=boundedInteger(bounds["min"],1,525600)
    max,okMax:=boundedInteger(bounds["max"],1,525600)
    return okMin && okMax && min<=max
}

// ECMAScript WhiteSpace and LineTerminator characters used by String.trim().
func ecmaSpace(r rune)bool{
    return unicode.Is(unicode.Zs,r) || strings.ContainsRune("\t\v\f\r\n\u2028\u2029\ufeff",r)
}

// encoding/json matches struct keys case-insensitively; the remote wire schema is exact.
func onlyKeys(raw json.RawMessage,names ...string)bool{
    var fields map[string]json.RawMessage
    if json.Unmarshal(raw,&fields)!=nil || fields==nil{return false}
    for key:=range fields{if !slices.Contains(names,key){return false}}
    return true
}
