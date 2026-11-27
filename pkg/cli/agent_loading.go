package cli

import (
    "fmt"
    "io"
    "sync"
    "time"
)

// Stop joins the renderer before clearing its line, so it cannot overwrite
// the selector or the remote terminal after control has been handed over.
func startAgentLoading(out io.Writer)func(){
    stop:=make(chan struct{});done:=make(chan struct{});var once sync.Once
    frames:=[]string{"⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"}
    dots:=[]string{"...",".",".."}
    fmt.Fprintf(out,"\r\x1b[2K%s LINKING TO YOUR REMOTE A2X BOT %s",frames[0],dots[0])
    go func(){
        defer close(done)
        ticker:=time.NewTicker(500*time.Millisecond);defer ticker.Stop()
        frame:=1
        for{select{
        case <-stop:return
        case <-ticker.C:fmt.Fprintf(out,"\r\x1b[2K%s LINKING TO YOUR REMOTE A2X BOT %s",frames[frame%len(frames)],dots[frame%len(dots)]);frame++
        }}
    }()
    return func(){once.Do(func(){close(stop);<-done;fmt.Fprint(out,"\r\x1b[2K")})}
}
