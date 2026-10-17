package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// LFSAvailable reports whether git-lfs is installed.
func LFSAvailable() bool {
	_, err := exec.LookPath("git-lfs")
	return err == nil
}

// LFSEnabled reports whether the repository has git-lfs set up: its filters
// are in the configuration.
func (r *Repo) LFSEnabled() bool {
	return r.Config("filter.lfs.clean") != "" || r.Config("filter.lfs.process") != ""
}

// LFSInstall sets git-lfs up for the repository.
func (r *Repo) LFSInstall() error {
	_, err := r.Do("lfs", "install", "--local")
	return err
}

// LFSPattern is a pattern git-lfs tracks, and the file that says so.
type LFSPattern struct {
	Pattern string
	Source  string
}

// LFSPatterns lists the tracked patterns.
func (r *Repo) LFSPatterns() ([]LFSPattern, error) {
	out, err := r.Read("lfs", "track")
	if err != nil {
		return nil, err
	}
	return parseLFSTrack(string(out)), nil
}

// parseLFSTrack reads "    *.psd (.gitattributes)".
func parseLFSTrack(out string) []LFSPattern {
	var list []LFSPattern
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "    ") {
			continue
		}
		line = strings.TrimSpace(line)
		i := strings.LastIndex(line, " (")
		if i < 0 || !strings.HasSuffix(line, ")") {
			continue
		}
		list = append(list, LFSPattern{Pattern: line[:i], Source: line[i+2 : len(line)-1]})
	}
	return list
}

// LFSTrack starts tracking a pattern.
func (r *Repo) LFSTrack(pattern string) error {
	_, err := r.Do("lfs", "track", pattern)
	return err
}

// LFSUntrack stops tracking a pattern.
func (r *Repo) LFSUntrack(pattern string) error {
	_, err := r.Do("lfs", "untrack", pattern)
	return err
}

// LFSFile is a file stored with git-lfs.
type LFSFile struct {
	OID  string
	Path string
	// Present is set when the file's content is in the work tree, not just
	// its pointer.
	Present bool
}

// LFSFiles lists the files of HEAD that git-lfs stores.
func (r *Repo) LFSFiles() ([]LFSFile, error) {
	if !r.HasHead() {
		return nil, nil
	}
	out, err := r.Read("lfs", "ls-files")
	if err != nil {
		return nil, err
	}
	return parseLFSFiles(string(out)), nil
}

// parseLFSFiles reads "abcdef0123 * path" ('*' for the content present, '-'
// for the pointer only).
func parseLFSFiles(out string) []LFSFile {
	var list []LFSFile
	for _, line := range strings.Split(out, "\n") {
		oid, rest, ok := strings.Cut(line, " ")
		if !ok || len(rest) < 3 {
			continue
		}
		list = append(list, LFSFile{OID: oid, Present: rest[0] == '*', Path: rest[2:]})
	}
	return list
}

// LFSStatus is what git-lfs says about the files that changed.
func (r *Repo) LFSStatus() (string, error) {
	out, err := r.Read("lfs", "status")
	return strings.TrimSpace(string(out)), err
}

// LFSFetch downloads the objects of the branch checked out, or of every ref
// with all set, from a remote ("" is the default).
func (r *Repo) LFSFetch(ctx context.Context, remote string, all bool) error {
	args := []string{"lfs", "fetch"}
	if all {
		args = append(args, "--all")
	}
	if remote != "" {
		args = append(args, remote)
	}
	_, err := r.DoContext(ctx, args...)
	return err
}

// LFSPull downloads the objects and checks them out in the work tree.
func (r *Repo) LFSPull(ctx context.Context, remote string) error {
	args := []string{"lfs", "pull"}
	if remote != "" {
		args = append(args, remote)
	}
	_, err := r.DoContext(ctx, args...)
	return err
}

// LFSPrune deletes the local objects that are old and pushed.
func (r *Repo) LFSPrune() (string, error) {
	out, err := r.Do("lfs", "prune")
	return strings.TrimSpace(string(out)), err
}

// LFSPointer reads a Git LFS pointer from a file's text: the oid and the
// size, ok false when the text is not a pointer.
func LFSPointer(text string) (oid string, size string, ok bool) {
	if !strings.HasPrefix(text, "version https://git-lfs.github.com/spec/v1") {
		return "", "", false
	}
	for _, l := range strings.Split(text, "\n") {
		if v, found := strings.CutPrefix(l, "oid sha256:"); found {
			oid = v
		}
		if v, found := strings.CutPrefix(l, "size "); found {
			size = v
		}
	}
	return oid, size, oid != ""
}

// PatchHasLFSPointer reports whether a patch changes a Git LFS pointer.
func PatchHasLFSPointer(patch string) bool {
	return strings.Contains(patch, "version https://git-lfs.github.com/spec/v1")
}

var errNoLFS = errors.New("git-lfs is not installed")

// ErrNoLFS is what the LFS commands report when git-lfs is missing.
func ErrNoLFS() error { return errNoLFS }
