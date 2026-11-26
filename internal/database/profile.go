package database

import (
    "context"
    "encoding/hex"
    "errors"
    "time"

    "github.com/leo1394/homebrew-janus/internal/deviceprofile"
    "github.com/leo1394/homebrew-janus/pkg/janus"
)

func metadataLiteral(value string)string{return "CONVERT(X'"+hex.EncodeToString([]byte(value))+"' USING utf8mb4)"}
func ProfileKey(database,table string)string{if database==""{return "overview"};key:="database:"+hex.EncodeToString([]byte(database));if table!=""{key="table:"+hex.EncodeToString([]byte(database))+":"+hex.EncodeToString([]byte(table))};return key}
// CollectProfile uses a dedicated connection, never the user's SQL transaction.
func(s *Sessions)CollectProfile(ctx context.Context,database,table string)(deviceprofile.Section,error){
    source:="mysql.information_schema"
    failure:=func(message string)(deviceprofile.Section,error){return deviceprofile.Failure(source,message),errors.New(message)}
    if len(database)>64||len(table)>64||table!=""&&database==""{return failure("invalid metadata selection")}
    request,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
    session,err:=s.Open(request);if err!=nil{return failure("metadata session unavailable")};defer s.Close(session.ID)
    queries:=map[string]string{}
    if database==""{
        queries["server"]="SELECT VERSION() AS version, @@character_set_client AS character_set_client, @@character_set_connection AS character_set_connection, @@character_set_results AS character_set_results, @@collation_connection AS collation_connection"
        queries["tls"]="SHOW SESSION STATUS LIKE 'Ssl_cipher'"
        queries["databases"]="SHOW DATABASES"
    }else if table==""{
        queries["tables"]="SELECT TABLE_NAME, TABLE_TYPE, TABLE_COMMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA="+metadataLiteral(database)+" ORDER BY TABLE_NAME LIMIT 1001"
    }else{
        where:=" WHERE TABLE_SCHEMA="+metadataLiteral(database)+" AND TABLE_NAME="+metadataLiteral(table)
        queries["columns"]="SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_COMMENT FROM information_schema.COLUMNS"+where+" ORDER BY ORDINAL_POSITION LIMIT 1001"
        queries["indexes"]="SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME FROM information_schema.STATISTICS"+where+" ORDER BY INDEX_NAME, SEQ_IN_INDEX LIMIT 1001"
        queries["foreignKeys"]="SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE"+where+" AND REFERENCED_TABLE_NAME IS NOT NULL LIMIT 1001"
    }
    data:=map[string]Result{};for key,sql:=range queries{result,err:=s.Query(request,session.ID,sql);if err!=nil{return failure("metadata query failed, denied or exceeded limits")};data[key]=result}
    if s.authorize!=nil{if err:=s.authorize();err!=nil{return failure("metadata authorization revoked")}}
    if s.admit!=nil{if err:=s.admit();err!=nil{return failure("metadata device changed")}}
    if !s.Accepting(){return failure("metadata collection stopped after disconnect")}
    return deviceprofile.Observation(source,"ok",data,15*time.Minute),nil
}
func(r Runtime)RefreshProfile(ctx context.Context,p janus.Profile,path,database,table string)error{
    client,err:=Dial(ctx,path);if err!=nil{return err};defer client.Close()
    response,err:=client.Call(ctx,Request{Method:"profile",Database:database,Table:table});if err!=nil{return err}
    if response.DeviceID!=p.ID||response.Binding!=Binding(p)||response.Profile==nil{return errors.New("metadata owner identity mismatch")}
    if err:=deviceprofile.Merge(r.Store,p,map[string]deviceprofile.Section{ProfileKey(database,table):*response.Profile});err!=nil{return err}
    if response.Profile.Status=="failed"{return errors.New(response.Profile.Error)};return nil
}
