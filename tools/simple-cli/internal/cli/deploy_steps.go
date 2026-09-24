package cli

import (
	"fmt"

	"simple-cli/internal/ui"
)

// Steps of the deploy and install plans.
const (
	stepConfig  ui.StepID = "config"
	stepConnect ui.StepID = "connect"
)

// connectNotices prints the re-authentication notice that deploy and install
// have always printed when the server rejects the cached token. The devops
// helpers report it as a note on the connect step; everything else they
// report is dropped, as before.
type connectNotices struct {
	ui.NopReporter
	quiet bool
}

// Note implements ui.StepReporter.
func (n connectNotices) Note(id ui.StepID, _ string) {
	if id == stepConnect && !n.quiet {
		fmt.Println("🔄 Auth token expired, refreshing...")
	}
}
