package app

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

// linkScheme is the URL scheme of the app, listed in urlSchemes of mygo.json.
// myfork://open?path=/repo opens the repository at /repo, once per path.
const linkScheme = "myfork"

// openLink is the deep link that opens the repositories at paths, which are
// absolute.
func openLink(paths []string) string {
	q := url.Values{}
	for _, p := range paths {
		q.Add("path", p)
	}
	return (&url.URL{Scheme: linkScheme, Host: "open", RawQuery: q.Encode()}).String()
}

// linkPaths are the repositories a deep link asks to open: none for a link
// that is no open link.
func linkPaths(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != linkScheme || u.Host != "open" {
		return nil
	}
	return u.Query()["path"]
}

// bundleOfCLI is the app bundle that the executable exe is the bundled CLI
// of, which sits at Contents/Resources/bin/myfork in it.
func bundleOfCLI(exe string) (string, bool) {
	bin := filepath.Dir(exe)
	if filepath.Base(exe) != cliName || filepath.Base(bin) != "bin" || filepath.Base(filepath.Dir(bin)) != "Resources" {
		return "", false
	}
	contents := filepath.Dir(filepath.Dir(bin))
	bundle := filepath.Dir(contents)
	return bundle, filepath.Base(contents) == "Contents" && strings.HasSuffix(bundle, ".app")
}

// launchApp opens the app bundle, and the deep link at it when not empty: the
// system starts the app if it does not run and hands it the link either way.
// Tests swap it out.
var launchApp = func(bundle, link string) error {
	args := []string{"-a", bundle}
	if link != "" {
		args = append(args, link)
	}
	if out, err := exec.Command("open", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("open %s: %w: %s", bundle, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runCLI is what the bundled CLI does in place of running the app, which
// would keep the terminal busy and end with it: it opens the repositories at
// args in the app of its bundle, and reports whether it was the CLI.
func runCLI(args []string) (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, nil
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	bundle, ok := bundleOfCLI(exe)
	if !ok {
		return false, nil
	}
	var link string
	if paths := cliPaths(args, ""); len(paths) > 0 {
		link = openLink(paths)
	}
	return true, launchApp(bundle, link)
}
