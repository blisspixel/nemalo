// Command package builds release archives from a clean, committed checkout.
package main

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/cli"
	"github.com/blisspixel/nemalo/internal/release"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	output := flag.String("output", filepath.Join("dist", cli.Version), "new release output directory")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("package accepts only --output DIRECTORY")
	}
	status, err := exec.CommandContext(ctx, "git", "status", "--porcelain").Output()
	if err != nil {
		return err
	}
	if len(status) != 0 {
		return fmt.Errorf("release packaging requires a clean committed checkout")
	}
	revision, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return err
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	goRoot, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}
	goLicense, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goRoot)), "LICENSE"))
	if err != nil {
		return err
	}
	moduleData, err := exec.CommandContext(ctx, "go", "list", "-m", "-json", "all").Output()
	if err != nil {
		return err
	}
	modules := make(map[string]release.Dependency)
	decoder := json.NewDecoder(strings.NewReader(string(moduleData)))
	for {
		var module struct {
			Path, Version, Dir string
			Main               bool
			Replace            *json.RawMessage
		}
		if err := decoder.Decode(&module); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if module.Replace != nil {
			return fmt.Errorf("release packaging does not accept module replacements")
		}
		if !module.Main {
			modules[module.Path] = release.Dependency{Path: module.Path, Version: module.Version, Directory: module.Dir}
		}
	}
	abs, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return err
	}
	if err := os.Mkdir(abs, 0700); err != nil {
		return err
	}
	var artifacts []release.Artifact
	for _, target := range release.Targets {
		name := filepath.Join(abs, ".build-"+target.OS+"-"+target.Arch)
		if target.OS == "windows" {
			name += ".exe"
		}
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=true", "-o", name, "./cmd/nemalo")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "GOOS=") && !strings.HasPrefix(value, "GOARCH=") && !strings.HasPrefix(value, "CGO_ENABLED=") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, "GOOS="+target.OS, "GOARCH="+target.Arch, "CGO_ENABLED=0")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		info, err := buildinfo.ReadFile(name)
		if err != nil {
			return err
		}
		settings := make(map[string]string)
		for _, setting := range info.Settings {
			settings[setting.Key] = setting.Value
		}
		if settings["GOOS"] != target.OS || settings["GOARCH"] != target.Arch || settings["CGO_ENABLED"] != "0" || settings["vcs.revision"] != strings.TrimSpace(string(revision)) || settings["vcs.modified"] != "false" {
			return fmt.Errorf("release binary metadata does not match the clean source revision and target")
		}
		var dependencies []release.Dependency
		for _, dep := range info.Deps {
			module, ok := modules[dep.Path]
			if !ok || module.Version != dep.Version || dep.Replace != nil {
				return fmt.Errorf("binary dependency does not match the pinned module graph: %s", dep.Path)
			}
			dependencies = append(dependencies, module)
		}
		notices, err := release.Notices(info.GoVersion, goLicense, dependencies)
		if err != nil {
			return err
		}
		binary, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		a, err := release.Archive(abs, cli.Version, target, binary, license, notices)
		if err != nil {
			return err
		}
		if err := os.Remove(name); err != nil {
			return err
		}
		artifacts = append(artifacts, a)
		fmt.Println(a.Name)
	}
	status, err = exec.CommandContext(ctx, "git", "status", "--porcelain").Output()
	if err != nil {
		return err
	}
	currentRevision, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return err
	}
	if len(status) != 0 || string(currentRevision) != string(revision) {
		return fmt.Errorf("source checkout changed during release packaging")
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	var checksums strings.Builder
	for _, a := range artifacts {
		fmt.Fprintf(&checksums, "%s  %s\n", a.SHA256, a.Name)
	}
	if err := release.WriteExclusive(filepath.Join(abs, "SHA256SUMS"), []byte(checksums.String())); err != nil {
		return err
	}
	record := fmt.Sprintf("Version: %s\nRevision: %s\nToolchain: %s\nCGO_ENABLED: 0\n\nArchives are cross-built. Native CI establishes only the tested runner architectures.\n", cli.Version, strings.TrimSpace(string(revision)), runtime.Version())
	return release.WriteExclusive(filepath.Join(abs, "BUILD.txt"), []byte(record))
}
