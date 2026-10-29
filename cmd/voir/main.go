// Command voir opens Git repositories in Voir, a native macOS git
// client drawn by MyGo's native UI.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/eliott/voir/app"
)

const usage = `Usage: voir [<path>...]

Open the Git repositories at <path> (default: the ones of the last run) in
tabs.
`

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" {
			fmt.Print(usage)
			return
		}
	}
	if err := app.Run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}
