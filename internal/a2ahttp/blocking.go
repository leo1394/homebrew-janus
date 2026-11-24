package a2ahttp

import (
    "context"
    "encoding/json"
    "time"
)
// Keep long waits in this HTTP adapter, not in the shared authenticated owner
// connection. Submit once with the original message identity; never resubmit on
// errors. Each poll is a fresh delegated call under the same grant/instance.
func(a *Adapter)sendMessage(ctx context.Context,params json.RawMessage)(json.RawMessage,int){
    var request map[string]json.RawMessage
    if json.Unmarshal(params,&request)!=nil||request==nil{return nil,-32602}
    configuration:=map[string]json.RawMessage{}
    if raw:=request["configuration"];len(raw)>0&&string(raw)!="null"{if json.Unmarshal(raw,&configuration)!=nil||configuration==nil{return nil,-32602}}
    var immediate bool
    if raw:=configuration["returnImmediately"];len(raw)>0{if json.Unmarshal(raw,&immediate)!=nil{return nil,-32602}}
    configuration["returnImmediately"]=json.RawMessage(`true`)
    request["configuration"],_=json.Marshal(configuration)
    submission,_:=json.Marshal(request)
    if ctx.Err()!=nil{return nil,-32603}
    result,code:=a.invoke(ctx,"SendMessage",submission);if code!=0||immediate{return result,code}
    var response map[string]json.RawMessage
    if json.Unmarshal(result,&response)!=nil{return nil,-32006}
    if value,ok:=response["message"];ok&&string(value)!="null"{if _,both:=response["task"];both{return nil,-32006};return result,0}
    task,ok:=response["task"];if !ok{return nil,-32006}
    id,state,valid:=taskState(task,"");if !valid{return nil,-32006}
    query:=map[string]json.RawMessage{};query["id"],_=json.Marshal(id)
    if tenant,ok:=request["tenant"];ok{query["tenant"]=tenant}
    if history,ok:=configuration["historyLength"];ok{query["historyLength"]=history}
    poll,_:=json.Marshal(query)
    interval:=a.PollInterval;if interval<=0{interval=time.Second}
    for state=="TASK_STATE_SUBMITTED"||state=="TASK_STATE_WORKING"{
        timer:=time.NewTimer(interval);select{case <-ctx.Done():timer.Stop();return nil,-32603;case <-timer.C:}
        if ctx.Err()!=nil{return nil,-32603}
        task,code=a.invoke(ctx,"GetTask",poll);if code!=0{return nil,code}
        _,state,valid=taskState(task,id);if !valid{return nil,-32006}
    }
    var final struct{Metadata map[string]json.RawMessage `json:"metadata"`;Status struct{Message json.RawMessage `json:"message"`} `json:"status"`}
    json.Unmarshal(task,&final)
    if state=="TASK_STATE_COMPLETED"&&string(final.Metadata["a2x.replyMode"])==`"message"`{
        var reply struct{Role string `json:"role"`;MessageID string `json:"messageId"`;ContextID string `json:"contextId"`;Parts []json.RawMessage `json:"parts"`}
        if json.Unmarshal(final.Status.Message,&reply)!=nil||reply.Role!="ROLE_AGENT"||reply.MessageID==""||reply.ContextID==""||len(reply.Parts)==0{return nil,-32006}
        result,_=json.Marshal(map[string]json.RawMessage{"message":final.Status.Message});return result,0
    }
    result,_=json.Marshal(map[string]json.RawMessage{"task":task});return result,0
}
func taskState(raw json.RawMessage,expected string)(string,string,bool){
    var task struct{ID string `json:"id"`;Status struct{State string `json:"state"`} `json:"status"`}
    if json.Unmarshal(raw,&task)!=nil||task.ID==""||expected!=""&&task.ID!=expected{return "","",false}
    switch task.Status.State{
    case "TASK_STATE_SUBMITTED","TASK_STATE_WORKING","TASK_STATE_COMPLETED","TASK_STATE_FAILED","TASK_STATE_CANCELED","TASK_STATE_REJECTED","TASK_STATE_INPUT_REQUIRED","TASK_STATE_AUTH_REQUIRED":return task.ID,task.Status.State,true
    }
    return "","",false
}
