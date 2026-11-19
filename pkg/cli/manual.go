package cli

import (
    _ "embed"
    "fmt"
    "strings"
    "github.com/leo1394/homebrew-janus/internal/metadata"
)

//go:embed janus.1
var Manual string

func Manpage()string{
    _,body,_:=strings.Cut(Manual,"\n")
    return fmt.Sprintf(".TH JANUS 1 %q %q \"User Commands\"\n",metadata.Date,"Janus "+metadata.Version)+body
}
