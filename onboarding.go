package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/viper"
)

type onboardingStep int

const (
	stepProvider onboardingStep = iota
	stepAPIKey
	stepModel
	stepShortcut
	stepFinalizing
)

type Provider struct {
	Name           string
	Description    string
	Endpoint       string
	ModelsEndpoint string
	RequiresKey    bool
}

var providers = []Provider{
	{
		Name:           "Ollama",
		Description:    "Local AI (requires Ollama running)",
		Endpoint:       "http://localhost:11434/api/chat",
		ModelsEndpoint: "http://localhost:11434/api/tags",
		RequiresKey:    false,
	},
	{
		Name:           "Pollinations",
		Description:    "Free AI (no key required)",
		Endpoint:       "https://text.pollinations.ai/openai",
		ModelsEndpoint: "",
		RequiresKey:    false,
	},
	{
		Name:           "Groq",
		Description:    "Ultra-fast inference (API key needed)",
		Endpoint:       "https://api.groq.com/openai/v1/chat/completions",
		ModelsEndpoint: "https://api.groq.com/openai/v1/models",
		RequiresKey:    true,
	},
	{
		Name:           "OpenRouter",
		Description:    "All models in one place (API key needed)",
		Endpoint:       "https://openrouter.ai/api/v1/chat/completions",
		ModelsEndpoint: "https://openrouter.ai/api/v1/models",
		RequiresKey:    true,
	},
}

type modelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"` // For Ollama
}

type modelsResponse struct {
	Data   []modelInfo `json:"data"`   // For OpenAI/Groq
	Models []modelInfo `json:"models"` // For Ollama
}

type onboardingModel struct {
	step        onboardingStep
	cursor      int
	modelCursor int
	textInput   textinput.Model
	provider    Provider
	apiKey      string
	models      []string
	loading     bool
	quitting    bool
	err         error
	ollamaDetected bool
}

func initialOnboardingModel() onboardingModel {
	ti := textinput.New()
	ti.Placeholder = "Paste your API key here..."
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256
	ti.Width = 50

	m := onboardingModel{
		step:      stepProvider,
		textInput: ti,
	}
	
	// Initial detection of Ollama
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err == nil && resp.StatusCode == http.StatusOK {
		m.ollamaDetected = true
		resp.Body.Close()
	}

	return m
}

type modelsFetchedMsg struct {
	models []string
	err    error
}

func (m onboardingModel) fetchModels() tea.Cmd {
	return func() tea.Msg {
		if m.provider.ModelsEndpoint == "" {
			return modelsFetchedMsg{models: []string{"openai"}, err: nil}
		}

		client := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest("GET", m.provider.ModelsEndpoint, nil)
		if m.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+m.apiKey)
		}

		resp, err := client.Do(req)
		if err != nil {
			return modelsFetchedMsg{err: err}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return modelsFetchedMsg{err: fmt.Errorf("API returned status %d", resp.StatusCode)}
		}

		var res modelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return modelsFetchedMsg{err: err}
		}

		var ids []string
		// Handle OpenAI/Groq/OpenRouter format
		for _, mi := range res.Data {
			ids = append(ids, mi.ID)
		}
		// Handle Ollama format
		for _, mi := range res.Models {
			ids = append(ids, mi.Name)
		}
		
		if len(ids) == 0 {
			return modelsFetchedMsg{err: fmt.Errorf("no models found")}
		}

		return modelsFetchedMsg{models: ids}
	}
}

func (m onboardingModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m onboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit

		case tea.KeyUp, tea.KeyLeft:
			if m.step == stepProvider && m.cursor > 0 {
				m.cursor--
			} else if m.step == stepModel && m.modelCursor > 0 {
				m.modelCursor--
			}

		case tea.KeyDown, tea.KeyRight:
			if m.step == stepProvider && m.cursor < len(providers)-1 {
				m.cursor++
			} else if m.step == stepModel && m.modelCursor < len(m.models)-1 {
				m.modelCursor++
			}

		case tea.KeyEnter:
			if m.step == stepProvider {
				m.provider = providers[m.cursor]
				if !m.provider.RequiresKey {
					m.apiKey = ""
					if m.provider.Name == "Pollinations" {
						viper.Set("model", "openai")
						m.step = stepFinalizing
						return m, m.saveConfig()
					}
					// For Ollama, go to model selection
					m.loading = true
					m.step = stepModel
					return m, m.fetchModels()
				}
				m.step = stepAPIKey
				m.textInput.Focus()
				return m, nil
			}

			if m.step == stepAPIKey {
				m.apiKey = m.textInput.Value()
				if m.apiKey == "" {
					return m, nil
				}
				m.loading = true
				m.step = stepModel
				return m, m.fetchModels()
			}

			if m.step == stepModel {
				if len(m.models) > 0 {
					viper.Set("model", m.models[m.modelCursor])
				}
				if runtime.GOOS == "windows" {
					m.step = stepShortcut
					return m, nil
				}
				m.step = stepFinalizing
				return m, m.saveConfig()
			}

			if m.step == stepShortcut {
				m.step = stepFinalizing
				return m, tea.Batch(m.installShortcut(), m.saveConfig())
			}
		}

	case modelsFetchedMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.models = []string{"custom (manual entry)"}
			return m, nil
		}
		m.models = msg.models
		m.err = nil
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m onboardingModel) installShortcut() tea.Cmd {
	return func() tea.Msg {
		if runtime.GOOS == "windows" {
			home, _ := os.UserHomeDir()
			profileDir := filepath.Join(home, "Documents", "WindowsPowerShell")
			_ = os.MkdirAll(profileDir, 0755)
			profilePath := filepath.Join(profileDir, "Microsoft.PowerShell_profile.ps1")
			
			aliasCmd := "\nfunction n { & \"$PSScriptRoot\\n.exe\" @args }\n"
			if exe, err := os.Executable(); err == nil {
				aliasCmd = fmt.Sprintf("\nfunction n { & \"%s\" @args }\n", exe)
			}

			f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err == nil {
				_, _ = f.WriteString(aliasCmd)
				f.Close()
			}
		}
		return nil
	}
}

func (m onboardingModel) saveConfig() tea.Cmd {
	return func() tea.Msg {
		viper.Set("provider", m.provider.Name)
		viper.Set("endpoint", m.provider.Endpoint)
		viper.Set("api_key", m.apiKey)
		
		_ = viper.WriteConfig()
		_ = viper.SafeWriteConfig()
		
		return tea.Quit()
	}
}

func (m onboardingModel) View() string {
	if m.quitting {
		return ""
	}

	var s strings.Builder

	grayStyle := lipgloss.NewStyle().Foreground(grayColor)
	successStyle := lipgloss.NewStyle().Foreground(successColor)

	s.WriteString(brandStyle.Render("NeuroCLI Setup") + "\n")
	s.WriteString(grayStyle.Render("Configure your AI workspace.") + "\n\n")

	switch m.step {
	case stepProvider:
		if m.ollamaDetected {
			s.WriteString(successStyle.Render("● Ollama detected locally") + "\n\n")
		}
		s.WriteString("Select your AI provider:\n\n")
		for i, p := range providers {
			cursor := " "
			style := lipgloss.NewStyle().PaddingLeft(2)
			if m.cursor == i {
				cursor = "➜"
				style = style.Foreground(accentColor).Bold(true)
			}
			s.WriteString(fmt.Sprintf("%s %s\n", cursor, style.Render(p.Name)))
			s.WriteString(fmt.Sprintf("    %s\n", grayStyle.Render(p.Description)))
		}

	case stepAPIKey:
		s.WriteString(fmt.Sprintf("Enter your %s API Key:\n\n", m.provider.Name))
		s.WriteString(m.textInput.View())
		s.WriteString("\n\n" + grayStyle.Render("Your key is stored locally in ~/.neurocli.yaml"))

	case stepModel:
		if m.loading {
			s.WriteString("Fetching available models...\n")
		} else if m.err != nil {
			s.WriteString(lipgloss.NewStyle().Foreground(errorColor).Render("Could not fetch models: ") + m.err.Error() + "\n")
			s.WriteString("Defaulting to manual configuration.\n")
			s.WriteString("\nPress Enter to continue.")
		} else {
			s.WriteString("Select a model:\n\n")
			start := 0
			if m.modelCursor > 10 {
				start = m.modelCursor - 5
			}
			end := start + 15
			if end > len(m.models) {
				end = len(m.models)
			}

			for i := start; i < end; i++ {
				cursor := " "
				style := lipgloss.NewStyle().PaddingLeft(2)
				if m.modelCursor == i {
					cursor = "➜"
					style = style.Foreground(accentColor).Bold(true)
				}
				s.WriteString(fmt.Sprintf("%s %s\n", cursor, style.Render(m.models[i])))
			}
		}

	case stepShortcut:
		s.WriteString("Install PowerShell alias?\n\n")
		s.WriteString(lipgloss.NewStyle().Foreground(accentColor).Render("➜ ") + "Set 'n' as a global command\n")
		s.WriteString("\n" + grayStyle.Render("This will add an alias to your PowerShell profile."))
		s.WriteString("\n" + grayStyle.Render("Press Enter to install, or Esc to skip."))

	case stepFinalizing:
		s.WriteString(successStyle.Render("✓ Configuration saved!") + "\n")
		s.WriteString("NeuroCLI is ready for action.")
	}

	return s.String()
}

func runOnboarding() error {
	p := tea.NewProgram(initialOnboardingModel())
	_, err := p.Run()
	return err
}
