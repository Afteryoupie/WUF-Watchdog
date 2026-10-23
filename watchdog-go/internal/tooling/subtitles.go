// SPDX-License-Identifier: Apache-2.0
package tooling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Subtitle struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

var markup = regexp.MustCompile(`<[^>]*>`)
var spaces = regexp.MustCompile(`\s+`)

func cleanSubtitle(text string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(markup.ReplaceAllString(text, "")), " "))
}
func subtitleTime(text string) (int64, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(text), ",", "."), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, errors.New("invalid subtitle timestamp")
	}
	var seconds float64
	for _, part := range parts {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil || n < 0 {
			return 0, errors.New("invalid subtitle timestamp")
		}
		seconds = seconds*60 + n
	}
	return int64(seconds*1000 + 0.5), nil
}

func ParseSubtitles(data []byte) ([]Subtitle, error) {
	var entries []Subtitle
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		var track struct {
			Events []struct {
				Start    int64 `json:"tStartMs"`
				Duration int64 `json:"dDurationMs"`
				Segs     []struct {
					Text string `json:"utf8"`
				} `json:"segs"`
			} `json:"events"`
		}
		if err := json.Unmarshal(data, &track); err != nil {
			return nil, err
		}
		for _, event := range track.Events {
			var text strings.Builder
			for _, s := range event.Segs {
				text.WriteString(s.Text)
			}
			if clean := cleanSubtitle(text.String()); clean != "" {
				entries = append(entries, Subtitle{event.Start, event.Start + max(event.Duration, 200), clean})
			}
		}
	} else {
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for i := 0; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if strings.HasPrefix(line, "NOTE") || strings.HasPrefix(line, "STYLE") {
				for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
					i++
				}
				continue
			}
			if !strings.Contains(line, "-->") {
				continue
			}
			bounds := strings.SplitN(line, "-->", 2)
			start, err := subtitleTime(bounds[0])
			if err != nil {
				return nil, err
			}
			fields := strings.Fields(bounds[1])
			if len(fields) == 0 {
				return nil, errors.New("missing subtitle end time")
			}
			end, err := subtitleTime(fields[0])
			if err != nil || end <= start {
				return nil, errors.New("invalid subtitle interval")
			}
			var text []string
			for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
				i++
				text = append(text, lines[i])
			}
			clean := cleanSubtitle(strings.Join(text, " "))
			if clean != "" {
				entries = append(entries, Subtitle{start, end, clean})
			}
		}
	}
	// WebVTT roll-up repeats the previous line in the next cue. Keep only new speech.
	var result []Subtitle
	previous := ""
	var previousEnd int64
	for _, entry := range entries {
		text := entry.Text
		overlap := entry.StartMS < previousEnd
		previousEnd = entry.EndMS
		if overlap && text == previous {
			continue
		}
		if overlap && previous != "" && strings.HasPrefix(text, previous+" ") {
			text = strings.TrimSpace(strings.TrimPrefix(text, previous))
		}
		previous = entry.Text
		entry.Text = text
		result = append(result, entry)
	}
	if len(result) == 0 {
		return nil, errors.New("no subtitle speech found (expected JSON3, VTT or SRT)")
	}
	return result, nil
}

func MergeSubtitles(entries []Subtitle) []Subtitle {
	var merged []Subtitle
	for _, entry := range entries {
		if len(merged) == 0 {
			merged = append(merged, entry)
			continue
		}
		last := &merged[len(merged)-1]
		runes := []rune(strings.TrimSpace(last.Text))
		punct := len(runes) > 0 && strings.ContainsRune(".!?。！？…", runes[len(runes)-1])
		if punct || len(strings.Fields(last.Text))+len(strings.Fields(entry.Text)) > 30 || entry.StartMS-last.EndMS >= 2500 {
			last.EndMS = min(last.EndMS, entry.StartMS)
			merged = append(merged, entry)
		} else {
			last.Text += " " + entry.Text
			last.EndMS = max(last.EndMS, entry.EndMS)
		}
	}
	return merged
}

type SubtitleResult struct {
	Subtitle
	Translation string   `json:"translation"`
	LatencyMS   int64    `json:"latency_ms"`
	Flags       []string `json:"flags,omitempty"`
	Error       string   `json:"error,omitempty"`
}
type SubtitleOptions struct {
	Input, Output, BaseURL, Model, Target, Prompt string
	Limit                                         int
	Timeout                                       time.Duration
}

// DownloadSubtitles uses an explicitly supplied standalone downloader, never uvx,
// a project Python script, a shell shortcut, browser cookies or global config.
func DownloadSubtitles(ctx context.Context, executable, videoURL, output string) (string, error) {
	parsed, err := url.Parse(videoURL)
	if err != nil || parsed.Scheme != "https" || (parsed.Hostname() != "www.youtube.com" && parsed.Hostname() != "youtube.com" && parsed.Hostname() != "youtu.be") {
		return "", errors.New("an HTTPS YouTube video URL is required")
	}
	if !filepath.IsAbs(executable) || !filepath.IsAbs(output) {
		return "", errors.New("absolute downloader and new output paths are required")
	}
	switch strings.ToLower(filepath.Ext(executable)) {
	case ".bat", ".cmd", ".command", ".py":
		return "", errors.New("use a standalone downloader executable, not a Python script or shell shortcut")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return "", err
	}
	if err := os.Mkdir(output, 0755); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, executable, "--ignore-config", "--skip-download", "--write-auto-subs", "--write-subs", "--sub-langs", "en-orig,en", "--sub-format", "json3", "--no-playlist", "--output", filepath.Join(output, "video.%(ext)s"), "--", videoURL)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return "", err
	}
	files, err := filepath.Glob(filepath.Join(output, "*.en-orig.json3"))
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		files, err = filepath.Glob(filepath.Join(output, "*.en.json3"))
	}
	if err != nil {
		return "", err
	}
	if len(files) != 1 {
		return "", errors.New("downloader did not produce one English JSON3 track")
	}
	return files[0], nil
}

func translationFlags(source, output string) []string {
	var flags []string
	if strings.TrimSpace(output) == "" {
		flags = append(flags, "empty")
	}
	if strings.EqualFold(strings.TrimSpace(source), strings.TrimSpace(output)) {
		flags = append(flags, "unchanged")
	}
	if strings.Contains(output, "---WUF-SEG---") || strings.Contains(output, "<<<WUF_SEGMENT_BOUNDARY>>>") || strings.Contains(output, "```") {
		flags = append(flags, "format_marker")
	}
	if !strings.Contains(source, "翻譯成") && strings.Contains(output, "翻譯成") {
		flags = append(flags, "instruction_wrapper")
	}
	return flags
}

func RunSubtitleDiagnostic(ctx context.Context, options SubtitleOptions) error {
	parsed, err := url.Parse(options.BaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("valid HTTP API base required")
	}
	if !filepath.IsAbs(options.Input) || !filepath.IsAbs(options.Output) {
		return errors.New("absolute input and output paths are required")
	}
	data, err := os.ReadFile(options.Input)
	if err != nil {
		return err
	}
	entries, err := ParseSubtitles(data)
	if err != nil {
		return err
	}
	entries = MergeSubtitles(entries)
	if options.Limit > 0 && len(entries) > options.Limit {
		entries = entries[:options.Limit]
	}
	if err := os.MkdirAll(filepath.Dir(options.Output), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(options.Output, 0755); err != nil {
		return err
	}
	client := &http.Client{Timeout: options.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	base := strings.TrimRight(options.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	target := options.Target
	if target == "zh-TW" {
		target = "Traditional Chinese"
	}
	if strings.TrimSpace(options.Prompt) == "" {
		options.Prompt = "Translate all speech into " + target + ". Output only the translation. Preserve names, brands, numbers and unfinished dialogue."
	}
	// Wake only the known WUF management port, never unrelated local servers.
	if (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1") && parsed.Port() == "8080" {
		wake, _ := http.NewRequestWithContext(ctx, "POST", parsed.Scheme+"://"+parsed.Host+"/api/wake", strings.NewReader("{}"))
		wake.Header.Set("Content-Type", "application/json")
		response, err := client.Do(wake)
		if err != nil {
			return err
		}
		var status struct{ Status, State string }
		decodeErr := json.NewDecoder(response.Body).Decode(&status)
		response.Body.Close()
		if decodeErr != nil || response.StatusCode != 200 || status.Status != "ok" || status.State != "running" {
			return errors.New("WUF wake did not report status=ok,state=running")
		}
	}
	var results []SubtitleResult
	for _, entry := range entries {
		body := map[string]any{"messages": []map[string]string{{"role": "system", "content": options.Prompt}, {"role": "user", "content": entry.Text}}, "wuf_target_language": target, "temperature": 0.2, "stream": false}
		if options.Model != "" && options.Model != "default" {
			body["model"] = options.Model
		}
		raw, _ := json.Marshal(body)
		request, _ := http.NewRequestWithContext(ctx, "POST", base+"/chat/completions", bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		started := time.Now()
		row := SubtitleResult{Subtitle: entry}
		response, err := client.Do(request)
		if err == nil {
			var answer struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
			response.Body.Close()
			if readErr != nil {
				err = readErr
			} else if response.StatusCode != 200 {
				err = fmt.Errorf("HTTP %d: %s", response.StatusCode, string(raw))
			} else if err = json.Unmarshal(raw, &answer); err == nil {
				if len(answer.Choices) == 0 {
					err = errors.New("missing choices")
				} else {
					row.Translation = strings.TrimSpace(answer.Choices[0].Message.Content)
					row.Flags = translationFlags(entry.Text, row.Translation)
					if answer.Choices[0].FinishReason == "length" {
						row.Flags = append(row.Flags, "truncated")
					}
				}
			}
		}
		row.LatencyMS = time.Since(started).Milliseconds()
		if err != nil {
			row.Error = err.Error()
		}
		results = append(results, row)
		fmt.Printf("%d/%d · %d ms · %s\n", len(results), len(entries), row.LatencyMS, row.Translation)
		// Save after each request so a cancelled run retains reviewable evidence.
		report, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(filepath.Join(options.Output, "results.json"), report, 0644); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	var output strings.Builder
	for i, row := range results {
		fmt.Fprintf(&output, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(row.StartMS), srtTime(row.EndMS), row.Translation)
	}
	if err := writeNew(filepath.Join(options.Output, "translated.srt"), []byte(output.String())); err != nil {
		return err
	}
	for _, row := range results {
		if row.Error != "" {
			return errors.New("one or more translations failed; inspect results.json")
		}
	}
	return nil
}
func srtTime(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}

// DecisionProbe replaces the old scratch script without a hardcoded cloud credential.
func DecisionProbe(ctx context.Context, endpoint, key, left, right string) (json.RawMessage, error) {
	if strings.TrimFunc(left, unicode.IsSpace) == "" || strings.TrimSpace(right) == "" {
		return nil, errors.New("both left and right subtitle fragments are required")
	}
	body := map[string]any{"model": "laya-multilingual", "state": map[string]any{"boundaries": []map[string]string{{"left": left, "right": right}}}, "questions": map[string]any{"boundary_0": map[string]string{"type": "noul", "instructions": "Does right start a new sentence?"}}}
	raw, _ := json.Marshal(body)
	request, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 130 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("decision HTTP %d: %s", response.StatusCode, string(data))
	}
	if !json.Valid(data) {
		return nil, errors.New("decision response is not JSON")
	}
	return data, nil
}
