package database

import (
    "context"
    "encoding/binary"
    "net"
    "strconv"
    "testing"
    "time"
)

// A server without CLIENT_SSL must never receive a password response, even
// when it advertises a supported password authentication plugin.
func TestNoPlaintextFallback(t *testing.T){
    listener,err:=net.Listen("tcp","127.0.0.1:0");if err!=nil{t.Fatal(err)};defer listener.Close()
    received:=make(chan int,1)
    go func(){
        conn,err:=listener.Accept();if err!=nil{received<- -1;return};defer conn.Close();conn.SetDeadline(time.Now().Add(3*time.Second))
        capabilities:=uint32(1|512|32768|524288)
        greeting:=[]byte{10};greeting=append(greeting,[]byte("8.0.0-test\x00")...);greeting=append(greeting,1,0,0,0)
        greeting=append(greeting,[]byte("12345678")...);greeting=append(greeting,0,byte(capabilities),byte(capabilities>>8),45,2,0,byte(capabilities>>16),byte(capabilities>>24),21)
        greeting=append(greeting,make([]byte,10)...);greeting=append(greeting,[]byte("abcdefghijkl\x00mysql_native_password\x00")...)
        header:=make([]byte,4);binary.LittleEndian.PutUint32(header,uint32(len(greeting)))
        if _,err:=conn.Write(append(header,greeting...));err!=nil{received<- -1;return}
        buffer:=make([]byte,256);n,_:=conn.Read(buffer);received<-n
    }()
    host,port,_:=net.SplitHostPort(listener.Addr().String());number,_:=strconv.Atoi(port)
    ctx,cancel:=context.WithTimeout(context.Background(),4*time.Second);defer cancel()
    db,err:=Open(ctx,Target{Host:host,Port:number,User:"fixture"},[]byte("synthetic-password"));if err==nil{db.Close();t.Fatal("plaintext server accepted")}
    if n:=<-received;n!=0{t.Fatalf("sent %d bytes to server without TLS",n)}
}

func TestExplicitTLSModes(t *testing.T){
    target:=Target{Host:"localhost",Port:3306,User:"fixture",TLSMode:"disabled"}
    if err:=ValidateTarget(target);err!=nil{t.Fatal(err)}
    target.CAPEM="certificate";if ValidateTarget(target)==nil{t.Fatal("disabled TLS accepted CA")}
    target.CAPEM="";target.TLSMode="preferred";if ValidateTarget(target)==nil{t.Fatal("automatic fallback accepted")}
}
