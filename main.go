package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/christophergyman/claude-quick/internal/config"
	"github.com/christophergyman/claude-quick/internal/devcontainer"
	"github.com/christophergyman/claude-quick/internal/tui"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Check for devcontainer CLI
	if err := devcontainer.CheckCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create TUI with async discovery - shows spinner while searching for projects
	// Tmux attachment happens within the TUI via tea.ExecProcess
	// When user detaches (Ctrl+b d), they return to the TUI dashboard
	model := tui.NewWithDiscovery(cfg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
