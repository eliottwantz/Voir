// MyFork is a fast and friendly git client, a reconstruction of Fork
// (fork.dev) drawn by MyGo's native UI.
//
//	myfork [<path>...]    open the repositories at <path> in tabs
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

	a := newApp(openStore())
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:          "MyFork",
			Width:          1280,
			Height:         820,
			MinWidth:       820,
			MinHeight:      480,
			StateKey:       "main",
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: titleBarHeight,
			Content:        ui.View(a.view),
		})
		mygo.App.SetMenu(a.buildMenu())
		if len(paths) > 0 {
			for _, p := range paths {
				a.openRepoReporting(p)
			}
		} else {
			a.restoreTabs()
		}
		a.win.OnFocus(func() { a.update(a.refresh) })
		a.win.OnClose(func(*mygo.CloseEvent) { a.saveTabs(); a.store.save() })
		a.win.OnFileDrop(func(e *mygo.FileDropEvent) {
			a.update(func() {
				for _, p := range e.Paths {
					a.openRepoReporting(p)
				}
			})
		})
		go a.autoFetch()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// autoFetch fetches the repository shown every few minutes, when the
// preference is on.
func (a *app) autoFetch() {
	for range time.Tick(5 * time.Minute) {
		a.update(func() {
			if !a.store.settings().AutoFetch {
				return
			}
			if t := a.tab(); t != nil && t.busy == "" && a.win.IsFocused() {
				t.fetch("")
			}
		})
	}
}
