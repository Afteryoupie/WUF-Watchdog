// SPDX-License-Identifier: Apache-2.0
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"wuf-translate/watchdog-go/internal/control"
	"wuf-translate/watchdog-go/internal/tooling"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func execute(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: wuf-tool control | package-mac | install-mac | icons | subtitles | decision | fetch-upstream | audit-python")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "control":
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		config := f.String("config", filepath.Join(filepath.Dir(executable), "launch.json"), "absolute packaged launch config")
		action := f.String("action", "status", "start, stop, status, startup-menu, enable-autostart, disable-autostart")
		testKey := f.String("test-registry-key", "", "isolated InstallTests GUID Run key; never a replacement production path")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		controlContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return control.Run(controlContext, *config, *action, *testKey, os.Stdin, os.Stdout)
	case "package-mac":
		repo := f.String("repo", "", "absolute source repository")
		output := f.String("output", "", "new absolute .app path")
		goExe := f.String("go", "", "absolute Go executable")
		swift := f.String("swiftc", "/usr/bin/swiftc", "absolute Swift compiler")
		layout := f.Bool("layout-only", false, "inspect resources without producing a runnable app")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if err := tooling.BuildMac(*repo, *output, *goExe, *swift, *layout); err != nil {
			return err
		}
		if *layout {
			fmt.Println("Resource layout only; NOT a runnable native app:", *output)
		} else {
			fmt.Println(*output)
		}
		return nil
	case "install-mac":
		app := f.String("app", "", "absolute installed .app path")
		action := f.String("action", "install", "install, uninstall, enable-autostart, disable-autostart")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if runtime.GOOS != "darwin" {
			return errors.New("install-mac must run on macOS as the logged-in user")
		}
		if !filepath.IsAbs(*app) {
			return errors.New("absolute .app path required")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		err = tooling.InstallMac(*action, *app, home, func() error {
			cmd := exec.CommandContext(ctx, "/bin/launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+tooling.LoginLabel)
			data, err := cmd.CombinedOutput()
			if err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) && (exit.ExitCode() == 3 || exit.ExitCode() == 113) {
					return nil
				}
				return fmt.Errorf("launchctl failed, entry preserved: %w: %s", err, data)
			}
			return nil
		})
		if err == nil {
			fmt.Println("Completed", *action, "without changing foreign registrations or model data.")
		}
		return err
	case "icons":
		vectors := f.String("vectors", "", "absolute watchdog-vectors.json path")
		output := f.String("output", "", "new absolute output directory")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if !filepath.IsAbs(*vectors) || !filepath.IsAbs(*output) {
			return errors.New("absolute vectors and output paths required")
		}
		return tooling.GenerateIcons(*vectors, *output)
	case "subtitles":
		input := f.String("sub-file", "", "absolute JSON3, VTT or SRT file")
		video := f.String("url", "", "optional HTTPS YouTube URL instead of --sub-file")
		downloader := f.String("downloader", "", "absolute standalone yt-dlp executable for --url")
		output := f.String("output-dir", "", "new absolute evidence directory")
		base := f.String("api-base", "http://127.0.0.1:8080/v1", "OpenAI-compatible API")
		model := f.String("model", "", "server-selected model by default")
		target := f.String("target", "zh-TW", "target language")
		limit := f.Int("limit", 30, "maximum sentence groups; 0 means all")
		timeout := f.Duration("timeout", 130*time.Second, "per-request timeout")
		prompt := f.String("prompt", "", "optional system prompt; default follows --target")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if *video != "" {
			if *input != "" {
				return errors.New("choose --url or --sub-file, not both")
			}
			if !filepath.IsAbs(*output) {
				return errors.New("absolute output directory required")
			}
			path, err := tooling.DownloadSubtitles(ctx, *downloader, *video, *output+"-captions")
			if err != nil {
				return err
			}
			*input = path
		}
		return tooling.RunSubtitleDiagnostic(ctx, tooling.SubtitleOptions{Input: *input, Output: *output, BaseURL: *base, Model: *model, Target: *target, Prompt: *prompt, Limit: *limit, Timeout: *timeout})
	case "decision":
		endpoint := f.String("endpoint", "http://127.0.0.1:8080/v1/systemone", "explicit decision API endpoint")
		left := f.String("left", "", "left subtitle fragment")
		right := f.String("right", "", "right subtitle fragment")
		keyEnv := f.String("key-env", "", "optional environment variable containing API key")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		key := ""
		if *keyEnv != "" {
			key = os.Getenv(*keyEnv)
			if key == "" {
				return errors.New("API key environment variable is empty")
			}
		}
		data, err := tooling.DecisionProbe(ctx, *endpoint, key, *left, *right)
		if err == nil {
			fmt.Println(string(data))
		}
		return err
	case "fetch-upstream":
		repo := f.String("repo", "", "absolute repository path")
		git := f.String("git", "", "absolute Git executable")
		remote := f.String("remote", "upstream", "configured remote name")
		ref := f.String("ref", "", "explicit branch or tag to fetch; never merges or overwrites files")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if !filepath.IsAbs(*repo) || !filepath.IsAbs(*git) {
			return errors.New("absolute repository and Git executable paths required")
		}
		for _, s := range []string{*remote, *ref} {
			if s == "" || strings.HasPrefix(s, "-") || strings.ContainsAny(s, "\r\n\x00") {
				return errors.New("explicit remote/ref without option prefixes required")
			}
		}
		cmd := exec.CommandContext(ctx, *git, "-C", *repo, "fetch", "--", *remote, *ref)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		fmt.Println("Fetched only. Review FETCH_HEAD before integrating; working files are unchanged.")
		return nil
	case "audit-python":
		repo := f.String("repo", "", "absolute repository root")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if !filepath.IsAbs(*repo) {
			return errors.New("absolute repository path required")
		}
		var unexpected []string
		allowed := map[string]bool{"watchdog/__init__.py": true, "watchdog/decision_protocol.py": true, "watchdog/laya_backend.py": true}
		err := filepath.WalkDir(*repo, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path == filepath.Join(*repo, "scripts", "output") {
					return filepath.SkipDir
				}
				switch d.Name() {
				case ".git", ".venv", "venv", "node_modules", "vendor", "dist", "outputs", "__pycache__", ".tools", "_backup", "backends":
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".py") {
				relative, err := filepath.Rel(*repo, path)
				if err != nil {
					return err
				}
				relative = filepath.ToSlash(relative)
				if allowed[relative] {
					fmt.Println("Laya inference:", relative)
				} else {
					unexpected = append(unexpected, relative)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(unexpected) > 0 {
			return fmt.Errorf("unexpected Python source files:\n%s", strings.Join(unexpected, "\n"))
		}
		fmt.Println("PASS: Python is limited to the three-file Laya inference package.")
		return nil
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}
