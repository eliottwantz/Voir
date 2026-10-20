// App packages up the window-level wiring of MyFork: it holds the app and
// opens the repositories at paths (default: the ones of the last run) in
// tabs, until the window closes.
package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run starts MyFork and runs until the window closes. The args are the CLI
// arguments: the paths to open in tabs (relative ones against the working
// directory).
func Run(args []string) error {
	// The bundled CLI only asks the app to open the paths, starting it first
	// when it does not run, so that it never holds the terminal.
	if ok, err := runCLI(args); ok {
		return err
	}
	// A second launch, such as `myfork .` in a terminal, hands its paths
	// over to the instance that runs and exits there.
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	a := newApp(openStore())
	// Deep links, such as the ones the CLI opens, come before the window
	// when they start the app: their paths wait for it.
	var waiting []string
	mygo.App.OnOpenURL(func(link string) {
		paths := linkPaths(link)
		if a.win == nil {
			waiting = append(waiting, paths...)
			return
		}
		a.win.Restore()
		a.win.Focus()
		a.update(func() {
			for _, p := range paths {
				a.openRepoReporting(p)
			}
		})
	})
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:                "MyFork",
			Width:                1280,
			Height:               820,
			MinWidth:             820,
			MinHeight:            480,
			StateKey:             "main",
			TitleBarStyle:        mygo.TitleBarHiddenInset,
			TitleBarHeight:       titleBarHeight,
			TrafficLightPosition: trafficLights,
			Vibrancy:             mygo.VibrancySidebar,
			Content:              ui.View(a.view),
			Page:                 mygo.PageOptions{DevTools: devTools()},
		})
		a.avatars.fetch, a.avatars.loaded = true, a.win.Invalidate
		mygo.App.SetMenu(a.buildMenu())
		paths := cliPaths(args, "")
		if len(paths) > 0 {
			for _, p := range paths {
				a.openRepoReporting(p)
			}
		} else {
			a.restoreTabs()
		}
		for _, p := range waiting {
			a.openRepoReporting(p)
		}
		waiting = nil
		a.win.OnFocus(func() { a.update(a.refresh) })
		a.win.OnClose(func(*mygo.CloseEvent) { a.saveTabs(); a.store.save() })
		a.win.OnFileDrop(func(e *mygo.FileDropEvent) {
			a.update(func() {
				for _, p := range e.Paths {
					a.openRepoReporting(p)
				}
			})
		})
		mygo.App.OnSecondInstance(func(args []string, wd string) {
			a.win.Restore()
			a.win.Focus()
			for _, p := range cliPaths(args, wd) {
				a.openRepoReporting(p)
			}
		})
		go a.autoFetch()
	})
	return mygo.App.Run()
}

// cliPaths are the paths to open among the CLI arguments args, made
// absolute against their working directory wd, without the flags of macOS
// app launches, such as the -psn arguments of Finder.
func cliPaths(args []string, wd string) []string {
	var paths []string
	for _, arg := range args {
		switch {
		case arg == "-h" || arg == "--help":
			// printed by the command, never a path to open
		case strings.HasPrefix(arg, "-psn_"):
			// what macOS passes when Finder starts an app
		default:
			p := arg
			if wd != "" && !filepath.IsAbs(p) {
				p = filepath.Join(wd, p)
			}
			p, err := filepath.Abs(p)
			if err == nil {
				paths = append(paths, p)
			}
		}
	}
	return paths
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

// devTools says whether the MyGo inspector is on. It is off, which frees ⇧⌘C
// (copy the link of a pull request) and ⌥⌘I; MYFORK_DEVTOOLS=1 turns it on, to
// work on the interface.
func devTools() mygo.DevTools {
	if os.Getenv("MYFORK_DEVTOOLS") != "" {
		return mygo.DevToolsEnabled
	}
	return mygo.DevToolsDisabled
}
