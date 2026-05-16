// NeuroCLI - AI-powered command line assistant to automate dev workflows and shell commands
// Created: May 2025
// Repository: github.com/Ravsalt/neurocli
// Version: 1.9.0
// Author: Raven <github.com/Ravsalt>

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:     "n",
		Aliases: []string{"neurocli"},
		Short:   "AI-powered command line assistant for developers",
		Long: `NeuroCLI is a premium command-line tool that brings AI capabilities to your terminal.
It helps with various development tasks including code generation, git operations, and more.`,
		Version: "1.9.0",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if cmd.Name() == "setup" || cmd.Name() == "help" || cmd.Name() == "version" {
				return
			}
			if viper.GetString("provider") == "" {
				fmt.Println(brandStyle.Render("First time? Let's get you set up.") + "\n")
				if err := runOnboarding(); err != nil {
					pterm.Error.Println("Onboarding failed:", err)
					os.Exit(1)
				}
			}
		},
	}

	brandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
)

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.neurocli.yaml)")

	rootCmd.AddCommand(newAskCmd())
	rootCmd.AddCommand(newGenerateCmd())
	rootCmd.AddCommand(newShellCmd())
	rootCmd.AddCommand(newAIDiffCmd())
	rootCmd.AddCommand(newAICommitCmd())
	rootCmd.AddCommand(newSetupCmd())

	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			fmt.Println(brandStyle.Render("NeuroCLI") + " - AI Assistant")
			return cmd.Help()
		}
		
		input := strings.Join(args, " ")
		if strings.HasPrefix(input, "!") {
			return executeCommand(strings.TrimSpace(strings.TrimPrefix(input, "!")))
		}

		// If it's not a known command, treat as an AI question
		msgs := []Message{{Role: "user", Content: input}}
		err := askAIStream(msgs, func(chunk string) {
			fmt.Print(chunk)
		})
		fmt.Println()
		return err
	}
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, _ := os.UserHomeDir()
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".neurocli")
	}
	viper.AutomaticEnv()
	_ = viper.ReadInConfig()
}

type StreamMsg struct {
	Content string
	Done    bool
	Err     error
}

func getDirContext() string {
	cwd, _ := os.Getwd()
	files, _ := os.ReadDir(cwd)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Current Directory: %s\nFiles:\n", cwd))
	for _, f := range files {
		info, _ := f.Info()
		prefix := "  - "
		if f.IsDir() {
			prefix = "  [DIR] "
		}
		sb.WriteString(fmt.Sprintf("%s%s (%d bytes)\n", prefix, f.Name(), info.Size()))
	}
	return sb.String()
}

func askAIStream(messages []Message, callback func(string)) error {
	endpoint := viper.GetString("endpoint")
	apiKey := viper.GetString("api_key")
	provider := viper.GetString("provider")
	model := viper.GetString("model")

	if endpoint == "" {
		endpoint = "https://text.pollinations.ai/openai"
	}

	// Prepare dynamic system prompt with context
	context := getDirContext()
	systemPrompt := fmt.Sprintf("NeuroCLI (%s). %s. Proactive agent. Tools (use on dedicated lines): 'Command: <cmd>', 'Read File: <path>', 'Write File: <path>'. Content for 'Write File' must follow in a markdown block. No trailing text after tool tags.", runtime.GOOS, context)
	
	fullMessages := append([]Message{{Role: "system", Content: systemPrompt}}, messages...)

	if model == "" {
		model = "openai"
		if provider == "Groq" {
			model = "llama3-70b-8192"
		}
	}

	reqData := ChatRequest{
		Model:       model,
		Messages:    fullMessages,
		Temperature: 0.7,
		MaxTokens:   2000,
		Stream:      true,
	}

	reqBody, _ := json.Marshal(reqData)
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if provider == "Ollama" {
			var ollamaResp struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}
			if err := json.Unmarshal([]byte(line), &ollamaResp); err != nil {
				continue
			}
			callback(ollamaResp.Message.Content)
			if ollamaResp.Done {
				break
			}
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var streamResp struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			continue
		}

		if len(streamResp.Choices) > 0 {
			callback(streamResp.Choices[0].Delta.Content)
		}
	}

	return nil
}

func askAI(prompt string) (string, error) {
	var fullResponse strings.Builder
	msgs := []Message{{Role: "user", Content: prompt}}
	err := askAIStream(msgs, func(chunk string) {
		fullResponse.WriteString(chunk)
	})
	
	if err != nil {
		provider := viper.GetString("provider")
		if provider == "Pollinations" || provider == "" {
			simpleAPI := "https://text.pollinations.ai/"
			encodedPrompt := url.PathEscape(prompt)
			resp, err := http.Get(simpleAPI + encodedPrompt)
			if err == nil && resp.StatusCode == http.StatusOK {
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				return string(body), nil
			}
		}
		return "", err
	}

	return fullResponse.String(), nil
}

func executeCommand(cmdStr string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func newAskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ask [prompt]",
		Short: "Ask a question to the AI",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			prompt := strings.Join(args, " ")
			msgs := []Message{{Role: "user", Content: prompt}}
			err := askAIStream(msgs, func(chunk string) {
				fmt.Print(chunk)
			})
			if err != nil {
				pterm.Error.Println(err)
			}
			fmt.Println()
		},
	}
}

func newGenerateCmd() *cobra.Command {
	var output, language string
	cmd := &cobra.Command{
		Use:   "gen [description]",
		Short: "Generate clean, production-ready code",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := fmt.Sprintf("Generate %s code for: %s. Return ONLY code.", language, strings.Join(args, " "))
			code, err := askAI(prompt)
			if err != nil {
				return err
			}
			code = cleanCodeResponse(code)
			if output == "" {
				fmt.Println(code)
				return nil
			}
			_ = os.MkdirAll(filepath.Dir(output), 0755)
			return os.WriteFile(output, []byte(code), 0644)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file")
	cmd.Flags().StringVarP(&language, "language", "l", "python", "Language")
	return cmd
}

func cleanCodeResponse(code string) string {
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "```") {
		lines := strings.Split(code, "\n")
		if len(lines) > 2 {
			code = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	return code
}

func newAIDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ai-diff",
		Short: "Explain git diff changes using AI",
		Run: func(cmd *cobra.Command, args []string) {
			explanation, err := AIDiff()
			if err != nil {
				pterm.Error.Println(err)
				return
			}
			pterm.Info.Println("Changes:")
			fmt.Println(explanation)
		},
	}
}

func newAICommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "aicommit",
		Short: "Generate a commit message",
		Run: func(cmd *cobra.Command, args []string) {
			message, err := AICommit()
			if err != nil {
				pterm.Error.Println(err)
				return
			}
			fmt.Println(message)
		},
	}
}

func newShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Start the premium interactive shell",
		Run: func(cmd *cobra.Command, args []string) {
			if err := handleShell(); err != nil {
				pterm.Error.Println(err)
			}
		},
	}
}

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Run the interactive onboarding process",
		Run: func(cmd *cobra.Command, args []string) {
			if err := runOnboarding(); err != nil {
				pterm.Error.Println(err)
			}
		},
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
