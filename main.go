package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	ollamaURL    = "http://localhost:11434/api/generate"
	defaultModel = "llama3.2"
	maxDiffChars = 12000
)

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

func getStageDiff() (string, error) {
	check := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	if err := check.Run(); err != nil {
		return "", fmt.Errorf("not a git repository")
	}

	cmd := exec.Command("git", "diff", "--staged")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git error: %v: %s", err, stderr.String())
	}

	return strings.TrimSpace(out.String()), nil
}

func buildPrompt(diff string) string {
	if len(diff) > maxDiffChars {
		diff = diff[:maxDiffChars] + "\n... (diff truncated)"
	}

	return `You are an expert at writing Git commit messages.
Write ONE commit message for the diff below using the Conventional Commits format.
Rules:
- First line: type(scope): short summary, max 72 characters, imperative mood
- Types: feat, fix, docs, style, refactor, test, chore
- Optionally add a blank line and 1-3 short bullet points
- Output ONLY the commit message, no explanations, no code fences
Diff:` + diff
}

func generateMessage(diff string) (string, error) {
	model := os.Getenv("COMMITGEN_MODEL")
	if model == "" {
		model = defaultModel
	}

	body, err := json.Marshal(ollamaRequest{
		Model:  model,
		Prompt: buildPrompt(diff),
		Stream: false,
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(ollamaURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("cannot reach Ollama (is it running?): %v", err)
	}

	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result ollamaResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("bad response from Ollama: %s", string(raw))
	}

	if result.Error != "" {
		return "", fmt.Errorf("ollama error:%s", result.Error)
	}

	msg := strings.TrimSpace(result.Response)
	msg = strings.Trim(msg, "`")

	return strings.TrimSpace(msg), nil

}

func main() {
	diff, err := getStageDiff()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if diff == "" {
		fmt.Fprintln(os.Stderr, "No staged changes. Run 'git add' first.")
		os.Exit(1)
	}

	fmt.Fprintln(os.Stderr, "Generating commit message...")
	msg, err := generateMessage(diff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println(msg)
}
