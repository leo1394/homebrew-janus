// Acceptance-only replacement for the authenticated SSH Remote transport.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"github.com/leo1394/homebrew-janus/internal/agentservice"
)

type remote struct { client *http.Client }
func (r remote) Done() <-chan struct{} { return nil }
func (r remote) Call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil { return err }
	req, err := http.NewRequestWithContext(ctx,"POST","http://fixture/"+method,bytes.NewReader(body))
	if err != nil { return err }
	resp, err := r.client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return errors.New("fixture denied") }
	return json.NewDecoder(resp.Body).Decode(out)
}
func main() {
	instance, err := os.ReadFile("/fixture/owner/instance")
	if err != nil { panic(err) }
	client := &http.Client{Transport:&http.Transport{DialContext:func(ctx context.Context,_,_ string)(net.Conn,error){
		return (&net.Dialer{}).DialContext(ctx,"unix","/fixture/owner/provider.sock")
	}}}
	err = agentservice.ServeIsolated(context.Background(),"/fixture/owner/service",string(instance),remote{client},agentservice.ServeOptions{DelegateDirectory:"/fixture/ipc",DelegateUIDs:[]uint32{24102}},nil)
	if err != nil { panic(err) }
}
