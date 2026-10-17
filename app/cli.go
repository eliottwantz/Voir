package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// cliName is the command name the CLI installs as.
const cliName = "myfork"

// cliPath is the home the CLI installs into, named for the user.
const cliPath = "~/.local/bin"

// installCLI symlinks this app's bundled CLI into ~/.local/bin, which makes
// `myfork` available there, and reports how it went.
func (a *app) installCLI() {
	a.background(func() {
		bin, err := cliBin()
		if err == nil {
			var cli string
			cli, err = a.cliExecutable()
			if err == nil {
				_, err = linkCLI(cli, bin)
				if err == nil {
					a.say("Installed the %s CLI at %s/%s", cliName, cliPath, cliName)
					return
				}
			}
		}
		a.update(func() { a.fail(fmt.Sprintf("Could not install the %s CLI", cliName), err) })
	})
}

// cliExecutable is the CLI this app installs: bin/myfork among its bundled
// resources, which is signed by mygo build with the app; without a bundle,
// as `mygo build` dev, its own binary.
func (a *app) cliExecutable() (string, error) {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err == nil {
		bin := filepath.Join(dir, "bin", cliName)
		if st, err := os.Stat(bin); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
			return bin, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// cliBin resolves the directory the CLI installs into, which must already
// exist there; tests swap it out.
var cliBin = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not find the home directory: %w", err)
	}
	bin := filepath.Join(home, ".local", "bin")
	st, err := os.Stat(bin)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s does not exist: create it, e.g. mkdir -p ~/.local/bin", bin)
	}
	return bin, nil
}

// linkCLI symlinks source into bin as the CLI, replacing an old symlink,
// and returns the link's path. A real file of the same name is left alone.
func linkCLI(source, bin string) (string, error) {
	link := filepath.Join(bin, cliName)
	if st, err := os.Lstat(link); err == nil {
		if st.Mode()&os.ModeSymlink == 0 {
			return "", fmt.Errorf("%s exists and is not a symlink", link)
		}
		if err := os.Remove(link); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(source, link); err != nil {
		return "", err
	}
	return link, nil
}
