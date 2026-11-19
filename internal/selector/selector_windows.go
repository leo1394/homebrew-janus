//go:build windows

package selector

import (
	"context"
	"fmt"
	"io"
	"os"
)

var servicePrompt = Prompt{Title: "Select device", EmptySelectionNotice: "Select one device before confirming."}

func SelectOne(ctx context.Context, in *os.File, out io.Writer, prompt Prompt, candidates []Candidate) (Candidate, bool, error) {
	return Candidate{}, false, fmt.Errorf("interactive device selection requires macOS or Linux; specify a device ID")
}

func SelectMany(ctx context.Context, in *os.File, out io.Writer, prompt Prompt, candidates []Candidate) ([]Candidate, bool, error) {
	return nil, false, fmt.Errorf("interactive device selection requires macOS or Linux; specify --device ID")
}
