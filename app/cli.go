package app

import (
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// cliName is the command name the CLI installs as.
const cliName = "voir"

// cliPath is the home the CLI installs into, named for the user.
const cliPath = "~/.local/bin"

// cliMarker is the line that tells an installed CLI script from a file of
// someone else's, which the install leaves alone. It is in voir.sh.
const cliMarker = "# Installed by Voir: Voir > Install `voir` CLI."

// cliScript is the CLI: the shell script voir.sh, not a binary, that opens
// the repositories at its arguments with a voir://open deep link. It
// prefers the dev app of `mygo dev`, which reloads, to the installed one, and
// the system finds either by its bundle identifier, so the script follows
// whichever builds exist. The bundle identifier is the one of mygo.json, and
// the dev app's is that with ".dev".
//
//go:embed voir.sh
var cliScript string

// installCLI writes the CLI script into ~/.local/bin, which makes `voir`
// available there, and reports how it went.
func (a *app) installCLI() {
	a.background(func() {
		bin, err := cliBin()
		if err == nil {
			_, err = writeCLI(bin)
			if err == nil {
				a.say("Installed the %s CLI at %s/%s", cliName, cliPath, cliName)
				return
			}
		}
		a.update(func() { a.fail(fmt.Sprintf("Could not install the %s CLI", cliName), err) })
	})
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

// writeCLI writes the CLI script into bin and returns its path. It replaces
// an old symlink, which is how the CLI was installed, or an older script; a
// real file of the same name that is no script of ours is left alone.
func writeCLI(bin string) (string, error) {
	path := filepath.Join(bin, cliName)
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSymlink == 0 {
			old, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			if !strings.Contains(string(old), cliMarker) {
				return "", fmt.Errorf("%s exists and is not the %s CLI", path, cliName)
			}
		}
		if err := os.Remove(path); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.WriteFile(path, []byte(cliScript), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// linkScheme is the URL scheme of the app, listed in urlSchemes of mygo.json.
// voir://open?path=/repo opens the repository at /repo, once per path;
// the CLI script builds these links.
const linkScheme = "voir"

// linkPaths are the repositories a deep link asks to open: none for a link
// that is no open link.
func linkPaths(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != linkScheme || u.Host != "open" {
		return nil
	}
	return u.Query()["path"]
}
