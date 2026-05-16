package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/viper"
)

// --- Design System ---

var (
	accentColor = lipgloss.Color("#7D56F4")
	grayColor   = lipgloss.Color("#626262")
	errorColor  = lipgloss.Color("#FF4C4C")
	successColor = lipgloss.Color("#4CAF50")

	titleStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true).MarginBottom(1)
	promptStyle = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	pathStyle = lipgloss.NewStyle().Foreground(grayColor).Italic(true)
	aiLabelStyle = lipgloss.NewStyle().Background(accentColor).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1).Bold(true).MarginRight(1)
	borderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(accentColor).PaddingLeft(2).MarginBottom(1)
	confirmStyle = lipgloss.NewStyle().Background(lipgloss.Color("#FFD700")).Foreground(lipgloss.Color("#000000")).Padding(0, 1).Bold(true).MarginRight(1)
)

// --- Types & Messages ---

type shellState int

const (
	stateInput shellState = iota
	stateLoading
	stateStreaming
	stateConfirmCommand
	stateConfirmRead
	stateConfirmWrite
)

type commandResultMsg struct {
	output string
	err    error
}

type streamSourceMsg struct {
	chunks chan string
	errs   chan error
}

type ShellCommand struct {
	Name        string
	Description string
	Handler     func([]string) error
}

// --- Model ---

type model struct {
	state       shellState
	textInput   textinput.Model
	spinner     spinner.Model
	lastInput   string
	fullOutput  *strings.Builder
	err         error
	cwd         string
	historyFile string
	commands    []ShellCommand
	renderer    *glamour.TermRenderer
	
	// Agentic Loop state
	messages     []Message
	pendingCmd   string
	pendingFile  string
	pendingContent string
	yoloMode     bool
	loopCount    int
	maxLoops     int
	
	// Streaming state
	chunks       chan string
	errs         chan error
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Ask Neuro or run a command..."
	ti.Prompt = ""
	ti.Focus()
	ti.CharLimit = 1024
	ti.Width = 80

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(accentColor)

	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)

	m := model{
		state:       stateInput,
		textInput:   ti,
		spinner:     s,
		cwd:         cwd,
		historyFile: filepath.Join(home, ".neurocli_history"),
		renderer:    renderer,
		fullOutput:  &strings.Builder{},
		yoloMode:    viper.GetBool("yolo"),
		messages:    []Message{},
		maxLoops:    5,
	}

	m.commands = []ShellCommand{
		{Name: "help", Description: "Show all commands", Handler: m.handleHelp},
		{Name: "clear", Description: "Clear the screen", Handler: m.handleClear},
		{Name: "cd", Description: "Change directory", Handler: m.handleChangeDir},
		{Name: "yolo", Description: "Toggle YOLO mode (auto-execute)", Handler: m.handleYolo},
		{Name: "setup", Description: "Run configuration setup", Handler: m.handleSetup},
		{Name: "reset", Description: "Clear conversation history", Handler: m.handleReset},
		{Name: "exit", Description: "Exit NeuroCLI", Handler: m.handleExit},
	}

	return m
}

// --- Commands ---

func startStreaming(messages []Message) tea.Cmd {
	return func() tea.Msg {
		chunks := make(chan string, 10)
		errs := make(chan error, 1)

		go func() {
			err := askAIStream(messages, func(chunk string) {
				chunks <- chunk
			})
			if err != nil {
				errs <- err
			}
			close(chunks)
		}()

		return streamSourceMsg{chunks: chunks, errs: errs}
	}
}

func waitForNextChunk(chunks chan string, errs chan error) tea.Cmd {
	return func() tea.Msg {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return StreamMsg{Done: true}
			}
			return StreamMsg{Content: chunk}
		case err := <-errs:
			return StreamMsg{Err: err}
		}
	}
}

func runShellCmdCaptured(input string) tea.Cmd {
	return func() tea.Msg {
		cmdStr := strings.TrimSpace(input)
		if strings.HasPrefix(cmdStr, "!") {
			cmdStr = strings.TrimSpace(cmdStr[1:])
		}
		
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}
		
		out, err := cmd.CombinedOutput()
		return commandResultMsg{output: string(out), err: err}
	}
}

// --- Update ---

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.state == stateInput {
				input := m.textInput.Value()
				if strings.TrimSpace(input) == "" {
					return m, nil
				}

				m.lastInput = input
				m.textInput.SetValue("")
				m.fullOutput.Reset()
				m.err = nil
				m.pendingCmd = ""
				m.pendingFile = ""
				m.loopCount = 0 // Reset loop on new user input
				
				if strings.HasPrefix(input, "!") {
					m.state = stateLoading
					return m, tea.Batch(m.spinner.Tick, runShellCmdCaptured(input))
				}

				parts := strings.Fields(input)
				for _, c := range m.commands {
					if c.Name == parts[0] {
						err := c.Handler(parts[1:])
						if err != nil {
							m.err = err
						}
						m.cwd, _ = os.Getwd()
						return m, nil
					}
				}

				m.messages = append(m.messages, Message{Role: "user", Content: input})
				m.state = stateLoading
				return m, tea.Batch(m.spinner.Tick, startStreaming(m.messages))
			}

		case tea.KeyRunes:
			if m.state == stateConfirmCommand {
				key := strings.ToLower(string(msg.Runes))
				if key == "y" {
					m.state = stateLoading
					cmd := m.pendingCmd
					m.pendingCmd = ""
					return m, tea.Batch(m.spinner.Tick, runShellCmdCaptured(cmd))
				} else if key == "n" {
					m.state = stateInput
					m.pendingCmd = ""
					return m, nil
				}
			}
			if m.state == stateConfirmRead {
				key := strings.ToLower(string(msg.Runes))
				if key == "y" {
					content, err := os.ReadFile(m.pendingFile)
					if err != nil {
						m.err = err
						m.state = stateInput
						return m, nil
					}
					m.messages = append(m.messages, Message{Role: "user", Content: fmt.Sprintf("Read File %s Output:\n%s", m.pendingFile, string(content))})
					m.state = stateLoading
					m.pendingFile = ""
					m.loopCount++
					return m, tea.Batch(m.spinner.Tick, startStreaming(m.messages))
				} else if key == "n" {
					m.state = stateInput
					m.pendingFile = ""
					return m, nil
				}
			}
			if m.state == stateConfirmWrite {
				key := strings.ToLower(string(msg.Runes))
				if key == "y" {
					_ = os.MkdirAll(filepath.Dir(m.pendingFile), 0755)
					err := os.WriteFile(m.pendingFile, []byte(m.pendingContent), 0644)
					if err != nil {
						m.err = err
						m.state = stateInput
					} else {
						m.messages = append(m.messages, Message{Role: "user", Content: fmt.Sprintf("Successfully wrote file %s", m.pendingFile)})
						m.state = stateLoading
						m.loopCount++
						return m, tea.Batch(m.spinner.Tick, startStreaming(m.messages))
					}
					m.pendingFile = ""
					m.pendingContent = ""
					return m, nil
				} else if key == "n" {
					m.state = stateInput
					m.pendingFile = ""
					m.pendingContent = ""
					return m, nil
				}
			}
		}

	case streamSourceMsg:
		m.state = stateStreaming
		m.chunks = msg.chunks
		m.errs = msg.errs
		return m, waitForNextChunk(m.chunks, m.errs)

	case StreamMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.state = stateInput
			return m, nil
		}
		if msg.Done {
			content := m.fullOutput.String()
			m.messages = append(m.messages, Message{Role: "assistant", Content: content})
			
			// Detect Tools & Auto-loop
			if m.loopCount < m.maxLoops {
				if cmd := extractCommand(content); cmd != "" {
					m.pendingCmd = cmd
					if m.yoloMode {
						m.state = stateLoading
						return m, tea.Batch(m.spinner.Tick, runShellCmdCaptured(m.pendingCmd))
					}
					m.state = stateConfirmCommand
					return m, nil
				}
				if path := extractTool(content, "Read File: "); path != "" {
					m.pendingFile = path
					m.state = stateConfirmRead
					return m, nil
				}
				if path := extractTool(content, "Write File: "); path != "" {
					m.pendingFile = path
					m.pendingContent = extractWriteContent(content)
					m.state = stateConfirmWrite
					return m, nil
				}
			}

			m.state = stateInput
			return m, nil
		}
		m.fullOutput.WriteString(msg.Content)
		return m, waitForNextChunk(m.chunks, m.errs)

	case commandResultMsg:
		if m.state == stateLoading && m.pendingCmd == "" { // If triggered by AI tool loop
			output := msg.output
			if output == "" {
				output = "(no output)"
			}
			if msg.err != nil {
				output = fmt.Sprintf("Error: %v\nOutput: %s", msg.err, output)
			}
			m.messages = append(m.messages, Message{Role: "user", Content: fmt.Sprintf("Command Output:\n%s", output)})
			m.state = stateLoading
			m.loopCount++
			return m, tea.Batch(m.spinner.Tick, startStreaming(m.messages))
		}
		
		// Fallback for manual '!' commands
		m.state = stateInput
		if msg.err != nil {
			m.err = msg.err
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func extractCommand(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Command: ") {
			cmd := strings.TrimSpace(strings.TrimPrefix(trimmed, "Command: "))
			// Only take the first line if the AI put text after the command on the same line
			if idx := strings.Index(cmd, "."); idx != -1 {
				cmd = cmd[:idx]
			}
			return strings.TrimSpace(cmd)
		}
	}
	return ""
}

func extractTool(content, prefix string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			if idx := strings.Index(val, " "); idx != -1 {
				val = val[:idx]
			}
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func extractWriteContent(content string) string {
	if idx := strings.Index(content, "```"); idx != -1 {
		endIdx := strings.LastIndex(content, "```")
		if endIdx > idx {
			block := content[idx:endIdx]
			lines := strings.Split(block, "\n")
			if len(lines) > 1 {
				return strings.Join(lines[1:], "\n")
			}
		}
	}
	return ""
}

// --- View ---

func (m model) View() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render("NeuroCLI Shell"))
	if m.yoloMode {
		s.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700")).Bold(true).Render(" [YOLO MODE]"))
	}
	if m.loopCount > 0 {
		s.WriteString(lipgloss.NewStyle().Foreground(accentColor).Render(fmt.Sprintf(" [LOOP %d/%d]", m.loopCount, m.maxLoops)))
	}
	s.WriteString("\n")

	relPath := m.cwd
	if home, err := os.UserHomeDir(); err == nil {
		if strings.HasPrefix(m.cwd, home) {
			relPath = "~" + strings.TrimPrefix(m.cwd, home)
		}
	}
	s.WriteString(pathStyle.Render(relPath))
	s.WriteString("\n\n")

	// Render Message History
	if len(m.messages) > 0 {
		start := 0
		if len(m.messages) > 6 {
			start = len(m.messages) - 6
		}
		for i := start; i < len(m.messages); i++ {
			msg := m.messages[i]
			if msg.Role == "user" {
				if strings.HasPrefix(msg.Content, "Command Output:") || strings.HasPrefix(msg.Content, "Read File") || strings.HasPrefix(msg.Content, "Successfully wrote") {
					// Hide raw tool outputs to keep UI clean, or show summarized
					s.WriteString(lipgloss.NewStyle().Foreground(grayColor).Italic(true).Render("➜ [System: Tool output received]") + "\n\n")
				} else {
					s.WriteString(promptStyle.Render("➜ "))
					s.WriteString(msg.Content + "\n\n")
				}
			} else {
				s.WriteString(aiLabelStyle.Render("NEURO"))
				s.WriteString("\n")
				rendered, _ := m.renderer.Render(msg.Content)
				s.WriteString(borderStyle.Render(rendered))
				s.WriteString("\n")
			}
		}
	}

	if m.state == stateStreaming {
		s.WriteString(aiLabelStyle.Render("NEURO"))
		s.WriteString("\n")
		content := m.fullOutput.String()
		if content == "" {
			s.WriteString(m.spinner.View() + " Thinking...")
		} else {
			rendered, _ := m.renderer.Render(content)
			s.WriteString(borderStyle.Render(rendered))
		}
		s.WriteString("\n")
	}

	if m.err != nil {
		s.WriteString(lipgloss.NewStyle().Foreground(errorColor).Render("Error: "+m.err.Error()) + "\n\n")
	}

	switch m.state {
	case stateLoading:
		s.WriteString(m.spinner.View() + " Agent at work...")
	case stateConfirmCommand:
		s.WriteString(confirmStyle.Render("EXECUTE?"))
		s.WriteString(fmt.Sprintf(" %s\n", m.pendingCmd))
		s.WriteString(lipgloss.NewStyle().Foreground(grayColor).Render("Press [y] to execute, [n] to cancel"))
	case stateConfirmRead:
		s.WriteString(confirmStyle.Render("READ FILE?"))
		s.WriteString(fmt.Sprintf(" %s\n", m.pendingFile))
		s.WriteString(lipgloss.NewStyle().Foreground(grayColor).Render("Press [y] to read, [n] to skip"))
	case stateConfirmWrite:
		s.WriteString(confirmStyle.Render("WRITE FILE?"))
		s.WriteString(fmt.Sprintf(" %s\n", m.pendingFile))
		s.WriteString(lipgloss.NewStyle().Foreground(grayColor).Render("Press [y] to write, [n] to cancel"))
	case stateInput:
		s.WriteString(promptStyle.Render("➜ "))
		s.WriteString(m.textInput.View())
	}

	return s.String()
}

// --- Command Handlers ---

func (m *model) handleReset(args []string) error {
	m.messages = []Message{}
	m.loopCount = 0
	return nil
}

func (m *model) handleYolo(args []string) error {
	m.yoloMode = !m.yoloMode
	viper.Set("yolo", m.yoloMode)
	_ = viper.WriteConfig()
	return nil
}

func (m *model) handleHelp(args []string) error {
	fmt.Println("\nAvailable Commands:")
	for _, c := range m.commands {
		fmt.Printf("  %-10s %s\n", c.Name, c.Description)
	}
	fmt.Println("  !command   Run a system command (not recorded)")
	fmt.Println()
	return nil
}

func (m *model) handleClear(args []string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd = exec.Command("clear")
	}
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func (m *model) handleChangeDir(args []string) error {
	if len(args) == 0 {
		home, _ := os.UserHomeDir()
		return os.Chdir(home)
	}
	return os.Chdir(args[0])
}

func (m *model) handleSetup(args []string) error {
	return runOnboarding()
}

func (m *model) handleExit(args []string) error {
	os.Exit(0)
	return nil
}

func handleShell() error {
	p := tea.NewProgram(initialModel())
	_, err := p.Run()
	return err
}
