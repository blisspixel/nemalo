package nativecheck

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/blisspixel/nemalo/internal/safeio"
)

type Binary struct {
	SHA256    string `json:"sha256"`
	Commit    string `json:"commit"`
	Platform  string `json:"platform"`
	GoVersion string `json:"go_version"`
	CGO       string `json:"cgo"`
	Clean     bool   `json:"clean"`
}

// Checkout finds the owning module, so invocation from a subdirectory cannot
// accidentally treat the rest of the checkout as outside validation output.
func Checkout(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		root, err := safeio.OpenResolvedRoot(dir)
		if err != nil {
			return "", err
		}
		before, statErr := root.Lstat("go.mod")
		if statErr == nil {
			f, openErr := safeio.OpenRegular(root, "go.mod", before, os.O_RDONLY)
			if openErr != nil {
				root.Close()
				return "", openErr
			}
			data, readErr := io.ReadAll(io.LimitReader(f, (64<<10)+1))
			err = errors.Join(readErr, f.Close(), root.Close())
			if err != nil {
				return "", err
			}
			if len(data) > 64<<10 {
				return "", errors.New("module marker exceeds its allowance")
			}
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[0] == "module" {
					if fields[1] == "github.com/blisspixel/nemalo" {
						return dir, nil
					}
					return "", errors.New("nativecheck must run in the Nemalo module")
				}
			}
			return "", errors.New("module marker has no module declaration")
		}
		err = root.Close()
		if !errors.Is(statErr, os.ErrNotExist) || err != nil {
			return "", errors.Join(statErr, err)
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", errors.New("module root for Nemalo was not found")
		}
		dir = next
	}
}

func validateBuild(info *debug.BuildInfo, commit string) (Binary, error) {
	var out Binary
	b, err := hex.DecodeString(commit)
	if err != nil || len(b) != 20 || hex.EncodeToString(b) != commit {
		return out, errors.New("expected commit must be a full lowercase 40-character SHA")
	}
	if info == nil || info.GoVersion != runtime.Version() || info.Path != "github.com/blisspixel/nemalo/cmd/nemalo" || info.Main.Path != "github.com/blisspixel/nemalo" || info.Main.Replace != nil {
		return out, errors.New("binary is not an unreplaced Nemalo command build")
	}
	settings := map[string]string{}
	for _, dep := range info.Deps {
		if dep.Replace != nil {
			return out, errors.New("binary dependency replacement is not accepted")
		}
	}
	for _, s := range info.Settings {
		if _, ok := settings[s.Key]; ok {
			return out, errors.New("duplicate build setting")
		}
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != runtime.GOOS || settings["GOARCH"] != runtime.GOARCH || settings["CGO_ENABLED"] != "0" || settings["vcs.revision"] != commit || settings["vcs.modified"] != "false" {
		return out, errors.New("binary must be cgo-free and match the clean expected commit and native target")
	}
	return Binary{Commit: commit, Platform: runtime.GOOS + "/" + runtime.GOARCH, GoVersion: info.GoVersion, CGO: "0", Clean: true}, nil
}

// Compare every existing parent identity as well as path spelling. This also
// rejects a checkout reached through an alias or a platform short name.
func outsideCheckout(checkout, parent string) error {
	root, err := safeio.Stat(checkout)
	if err != nil {
		return err
	}
	if !root.IsDir() {
		return errors.New("checkout must be an existing directory")
	}
	for p := parent; ; p = filepath.Dir(p) {
		info, err := safeio.Stat(p)
		if err != nil {
			return err
		}
		if os.SameFile(root, info) {
			return errors.New("native validation output must be outside the checkout")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

type workspace struct {
	root      *os.Root
	directory string
}

func newWorkspace(checkout, output string) (*workspace, error) {
	if output == "" {
		parent, err := filepath.EvalSymlinks(os.TempDir())
		if err != nil {
			return nil, err
		}
		if err = outsideCheckout(checkout, parent); err != nil {
			return nil, err
		}
		name, err := os.MkdirTemp(parent, "nemalo-native-check-")
		if err != nil {
			return nil, err
		}
		root, err := safeio.OpenRoot(name)
		if err != nil {
			return nil, err
		}
		return &workspace{root, name}, nil
	}
	if !filepath.IsAbs(output) {
		return nil, errors.New("output must be an absolute new directory")
	}
	parent, err := safeio.OpenResolvedRoot(filepath.Dir(output))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	physical, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return nil, err
	}
	physical, err = filepath.Abs(physical)
	if err != nil {
		return nil, err
	}
	observed, err := safeio.Stat(physical)
	if err != nil {
		return nil, err
	}
	opened, err := parent.Stat(".")
	if err != nil {
		return nil, err
	}
	if !os.SameFile(observed, opened) {
		return nil, errors.New("workspace parent changed while resolved")
	}
	if err = outsideCheckout(checkout, physical); err != nil {
		return nil, err
	}
	name := filepath.Base(output)
	if name == "." || name == ".." {
		return nil, errors.New("output must name a new directory")
	}
	if err = parent.Mkdir(name, 0700); err != nil {
		return nil, fmt.Errorf("exclusive output directory: %w", err)
	}
	root, err := safeio.OpenRootIn(parent, name)
	if err != nil {
		return nil, err
	}
	return &workspace{root, filepath.Join(physical, name)}, nil
}

func prepareBinary(root *os.Root, name, commit string) (out Binary, executable string, err error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return out, "", err
	}
	parent, err := safeio.OpenResolvedRoot(filepath.Dir(abs))
	if err != nil {
		return out, "", err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	base := filepath.Base(abs)
	before, err := parent.Lstat(base)
	if err != nil {
		return out, "", err
	}
	if !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 128<<20 {
		return out, "", errors.New("binary must be a regular file no larger than 128 MiB")
	}
	source, err := safeio.OpenRegular(parent, base, before, os.O_RDONLY)
	if err != nil {
		return out, "", err
	}
	defer func() { err = errors.Join(err, source.Close()) }()
	info, err := buildinfo.Read(source)
	if err != nil {
		return out, "", err
	}
	out, err = validateBuild(info, commit)
	if err != nil {
		return out, "", err
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return out, "", err
	}
	executable = "native-nemalo"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	copy, err := root.OpenFile(executable, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return out, "", err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(copy, hash), io.LimitReader(source, before.Size()+1))
	err = errors.Join(copyErr, copy.Close())
	if err != nil {
		return out, "", err
	}
	after, err := source.Stat()
	if err != nil {
		return out, "", err
	}
	named, err := parent.Lstat(base)
	if err != nil {
		return out, "", err
	}
	if n != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || !os.SameFile(after, named) {
		return out, "", errors.New("source executable changed while copied")
	}
	out.SHA256 = hex.EncodeToString(hash.Sum(nil))
	file, err := safeio.OpenRegular(root, executable, nil, os.O_RDONLY)
	if err != nil {
		return out, "", err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err = buildinfo.Read(file)
	if err != nil {
		return out, "", err
	}
	if _, err = validateBuild(info, commit); err != nil {
		return out, "", err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return out, "", err
	}
	hash.Reset()
	if _, err = io.Copy(hash, io.LimitReader(file, n+1)); err != nil {
		return out, "", err
	}
	if hex.EncodeToString(hash.Sum(nil)) != out.SHA256 {
		return out, "", errors.New("private executable copy differs")
	}
	return out, executable, nil
}

func childEnvironment(root string) []string {
	return isolatedEnvironment(root, os.Environ())
}

func isolatedEnvironment(root string, inherited []string) []string {
	var env []string
	for _, value := range inherited {
		key, _, _ := strings.Cut(value, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "NEMALO_") || strings.HasPrefix(key, "XDG_") {
			continue
		}
		switch key {
		case "APPDATA", "LOCALAPPDATA", "TMPDIR", "TEMP", "TMP", "PATH", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432", "COMMONPROGRAMFILES", "COMMONPROGRAMFILES(X86)", "COMMONPROGRAMW6432", "ALLUSERSPROFILE":
			continue
		}
		env = append(env, value)
	}
	for key, dir := range map[string]string{"XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_CACHE_HOME": "cache", "APPDATA": "config", "LOCALAPPDATA": "state", "TMPDIR": "temp", "TEMP": "temp", "TMP": "temp", "PATH": "tools", "ProgramData": "tools", "ProgramFiles": "tools", "ProgramFiles(x86)": "tools", "ProgramW6432": "tools", "CommonProgramFiles": "tools", "CommonProgramFiles(x86)": "tools", "CommonProgramW6432": "tools", "ALLUSERSPROFILE": "tools"} {
		env = append(env, key+"="+filepath.Join(root, dir))
	}
	return append(env, "NEMALO_CONFIG="+filepath.Join(root, "config.json"))
}
