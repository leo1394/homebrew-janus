package agentservice

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestDispatchWire(t *testing.T){
    submit:=`{"idempotencyKey":"k","profile":"pi-report","text":"test"`
    ctx:=`,"dispatchContext":{"dispatchId":"D-1","workPackageId":"W.1","nodeRevision":3,"estimatedMinutes":{"min":20,"max":30},"estimateBasis":"prior runs"}}`
    check:=func(method,body string,want bool){
        t.Helper()
        err:=Validate(Request{GrantID:"g",Credential:"c",Method:method,Params:json.RawMessage(body)},false)
        if (err==nil)!=want{t.Fatal(method,body,err)}
    }
    check("tasks.submit",submit+"}",true)
    check("tasks.submit",submit+ctx,true)
    check("tasks.submit",submit+strings.Replace(ctx,"dispatchContext","DispatchContext",1),false)
    check("tasks.submit",submit+strings.Replace(ctx,"nodeRevision","NodeRevision",1),false)
    check("tasks.events",`{"taskId":"id","After":2}`,false)
    check("tasks.submit",submit+strings.Replace(ctx,`"nodeRevision"`,`"workItemId":"part-1","nodeRevision"`,1),true)
    check("tasks.submit",submit+strings.Replace(ctx,`"nodeRevision"`,`"workItemId":"","nodeRevision"`,1),false)
    check("tasks.submit",submit+strings.Replace(ctx,`{"min":20,"max":30}`,"1",1),true)
    for _,change:=range [][2]string{{"20","31"},{"30","525601"},{"D-1","D/1"},{"prior runs",strings.Repeat("文",513)},{"Revision\":3","Revision\":0"},{"estimateBasis","unexpected"}}{
        check("tasks.submit",submit+strings.Replace(ctx,change[0],change[1],1),false)
    }
    check("tasks.submit",submit+`,"dispatchContext":null}`,false)
    for _,change:=range [][2]string{{"20","2e1"},{"30","30.0"},{"Revision\":3","Revision\":3.0"},{"prior runs",`\u0085`}}{
        check("tasks.submit",submit+strings.Replace(ctx,change[0],change[1],1),true)
    }
    for _,basis:=range []string{`x\u0000y`,`\ufeff\u2029`}{check("tasks.submit",submit+strings.Replace(ctx,"prior runs",basis,1),false)}
    check("tasks.events",`{"taskId":"id","after":1e1}`,true)
    check("tasks.events",`{"taskId":"id"}`,true)
    check("tasks.events",`{"taskId":"id","after":0}`,true)
    for _,value:=range []string{"null","-1","1.2","9007199254740992"}{check("tasks.events",`{"taskId":"id","after":`+value+"}",false)}
}
