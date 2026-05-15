# NeuroCLI

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/Ravsalt/neurocli)](https://goreportcard.com/report/github.com/Ravsalt/neurocli)
[![Go Reference](https://pkg.go.dev/badge/github.com/Ravsalt/neurocli.svg)](https://pkg.go.dev/github.com/Ravsalt/neurocli)

Go-based AI CLI tools to automate dev workflows and shell commands.

NeuroCLI is an AI-powered command line assistant designed to enhance developer productivity by providing intelligent assistance directly in your terminal. It combines the power of AI with practical command-line utilities to streamline development workflows.

## 🌟 Features

- **Natural Language Processing**: Interact with AI using natural language
- **Code Generation**: Generate code snippets in multiple programming languages
- **Git Integration**: AI-powered git operations (diffs, commits, etc.)
- **Interactive Shell**: Built-in shell with command history and completion
- **Cross-Platform**: Works on Windows, macOS, and Linux

## 🚀 Installation

### Prerequisites

- Go 1.16 or later
- Git (for version control features)

### Using Go (Recommended)

1. Ensure you have Go 1.16 or later installed. Check your Go version:
   ```bash
   go version
   ```

2. Install the latest version of NeuroCLI:
   ```bash
   go install github.com/ravvdevv/neurocli@latest
   ```

3. Add Go's bin directory to your PATH if it's not already there:
   - Linux/macOS: Add this to your `~/.bashrc`, `~/.zshrc`, or equivalent:
     ```bash
     export PATH="$PATH:$(go env GOPATH)/bin"
     ```
   - Windows: Add `%USERPROFILE%\go\bin` to your system's PATH

4. Verify the installation:
   ```bash
   neurocli --version
   ```
   Or check the help:
   ```bash
   neurocli --help
   ```

5. You can now use NeuroCLI from any terminal window. Try it out:
   ```bash
   neurocli ask "how do I sort a map in Go?"
   ```

### Building from Source

```bash
git clone https://github.com/Ravsalt/neurocli.git
cd neurocli
go build -o neurocli .
sudo mv neurocli /usr/local/bin/  # Optional: Add to PATH
```

## 🛠️ Usage

### Interactive Shell

Launch the interactive shell:

```bash
neurocli shell
```

### Generate Code

Generate code from a description:

```bash
neurocli gen -l python -o script.py "function that reverses a string"
```

### Git Operations

Generate a commit message for staged changes:

```bash
git add .
neurocli aicommit
```

Explain git changes:

```bash
neurocli ai-diff
```

### Ask Questions

Get answers to programming questions:

```bash
neurocli ask "how do I sort a map in Go?"
```

## 🏗️ Built With

- [Cobra](https://github.com/spf13/cobra) - CLI framework
- [Pterm](https://github.com/pterm/pterm) - Beautiful terminal output
- [Liner](https://github.com/peterh/liner) - Command line editor
- [Viper](https://github.com/spf13/viper) - Configuration management

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3. Commit your Changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the Branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 👏 Acknowledgments

- [Pollinations AI](https://github.com/pollinations/pollinations) for the free and open source AI API

## 📧 Contact

Raven - [@Ravsalt](https://github.com/Ravsalt)

Project Link: [https://github.com/Ravsalt/neurocli](https://github.com/Ravsalt/neurocli)
