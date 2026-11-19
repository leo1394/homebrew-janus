package ssh

import (
    "bufio"
    "fmt"
    "io"
    "os"
    "strings"
    "time"

    "github.com/leo1394/homebrew-janus/internal/timing"
)

// Raw OpenSSH diagnostics stay in a private temporary file and are deleted.
// Only fixed milestone names enter Janus output, never prompts or credentials.
func traceSSH(diagnostics io.Writer) (string,func(bool),error) {
    f,err:=os.CreateTemp("","janus-ssh-trace-");if err!=nil{return "",nil,err}
    path:=f.Name();done:=make(chan struct{});finished:=make(chan struct{})
    go func(){
        defer close(finished)
        reader:=bufio.NewReader(f);pending:="";seen:=map[string]bool{}
        consume:=func(){for{line,err:=reader.ReadString('\n');pending+=line;if strings.HasSuffix(pending,"\n"){
            for _,m:=range [][2]string{{"Connecting to ","tcp_connect_start"},{"Connection established.","tcp_connected"},{"Remote protocol version","server_banner_received"},{"SSH2_MSG_KEXINIT sent","key_exchange_start"},{"SSH2_MSG_KEXINIT received","key_exchange_proposal_received"},{"expecting SSH2_MSG_KEX_ECDH_REPLY","key_exchange_reply_wait"},{"SSH2_MSG_KEX_ECDH_REPLY received","key_exchange_reply_received"},{"SSH2_MSG_NEWKEYS sent","client_newkeys_sent"},{"SSH2_MSG_NEWKEYS received","key_exchange_complete"},{"SSH2_MSG_SERVICE_ACCEPT received","auth_service_ready"},{"send packet: type 50","auth_probe_sent"},{"Authentications that can continue:","auth_methods_received"},{"Next authentication method: password","password_method_selected"},{"we sent a password packet, wait for reply","password_packet_sent"},{"read_passphrase: requested to askpass","askpass_launch_requested"},{"Authenticated to ","ssh_authenticated"},{"Entering interactive session","ssh_session_entered"},{"mux_client_request_session: master session id","mux_session_accepted"},{"mux_client_hello_exchange: master version","mux_control_connected"},{"mux_client_request_session: session request sent","mux_session_requested"},{"Timeout, server","server_keepalive_timeout"},{"Broken pipe","transport_broken_pipe"},{"Connection reset","transport_reset"}}{
                if strings.Contains(pending,m[0])&&!seen[m[1]]{seen[m[1]]=true;timing.Mark(m[1])}
            };pending=""
        };if err!=nil{return}}}
        ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
        for{select{case <-ticker.C:consume();case <-done:consume();return}}
    }()
    return path,func(failed bool){
        close(done);<-finished
        if failed && diagnostics!=nil {
            // -E also redirects fatal SSH messages. Replay non-debug lines so
            // users see the real failure, rather than only exit status 255.
            f.Seek(0,0)
            scanner:=bufio.NewScanner(io.LimitReader(f,1<<20))
            for scanner.Scan(){line:=scanner.Text();if line!=""&&!strings.HasPrefix(line,"debug"){fmt.Fprintln(diagnostics,line)}}
        }
        f.Close();os.Remove(path)
    },nil
}
