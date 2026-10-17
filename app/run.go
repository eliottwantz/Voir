// App packages up the window-level wiring of MyFork: it holds the app and
// opens the repositories at paths (default: the ones of the last run) in
// tabs, until the window closes.
package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run starts MyFork and runs until the window closes.
func Run(paths []string) error {
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
			Vibrancy:       mygo.VibrancySidebar,
			Content:        ui.View(a.view),
		})
		a.avatars.fetch, a.avatars.loaded = true, a.win.Invalidate
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
	return mygo.App.Run()
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
