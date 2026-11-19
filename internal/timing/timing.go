package timing

import (
    "fmt"
    "os"
    "strconv"
    "time"
)
var Start = time.Now()
func Epoch()string{return strconv.FormatFloat(float64(Start.UnixNano())/1e9,'f',6,64)}
func Mark(phase string){if os.Getenv("JANUS_TRACE")=="1"{fmt.Fprintf(os.Stderr,"janus phase=%s elapsed_ms=%d\n",phase,time.Since(Start).Milliseconds())}}
