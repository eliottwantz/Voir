// Command myfork opens Git repositories in MyFork, a native macOS git
// client drawn by MyGo's native UI.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/eliott/myfork/app"
)

const usage = `Usage: myfork [<path>...]

Open the Git repositories at <path> (default: the ones of the last run) in
tabs.
`

func main() {
	var paths []string
	for _, arg := range os.Args[1:] {
		switch {
		case arg == "-h" || arg == "--help":
			fmt.Print(usage)
			return
		case len(arg) > 5 && arg[:5] == "-psn_": // what macOS passes to apps opened from Finder
		default:
			if abs, err := filepath.Abs(arg); err == nil {
				paths = append(paths, abs)
			}
		}
	}
	if err := app.Run(paths); err != nil {
		log.Fatal(err)
	}
}
