package tui

import (
	"fmt"
	"strings"

	"github.com/christophergyman/claude-quick/internal/config"
)

// RenderConfigDisplay renders the configuration view
func RenderConfigDisplay(cfg *config.Config) string {
	var b strings.Builder

	title := TitleStyle.Render("claude-quick")
	subtitle := SubtitleStyle.Render("Configuration")

	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(subtitle)
	b.WriteString("\n\n")

	// Config file location
	b.WriteString(DimmedStyle.Render("Config file: "))
	b.WriteString(config.ConfigPath())
	b.WriteString("\n\n")

	// Search Paths
	b.WriteString(SuccessStyle.Render("Search Paths:"))
	b.WriteString("\n")
	for _, p := range cfg.SearchPaths {
		b.WriteString("  " + p + "\n")
	}
	b.WriteString("\n")

	// Max Depth
	b.WriteString(SuccessStyle.Render("Max Depth: "))
	b.WriteString(fmt.Sprintf("%d", cfg.MaxDepth))
	b.WriteString("\n\n")

	// Excluded Dirs (show all)
	b.WriteString(SuccessStyle.Render("Excluded Dirs:"))
	b.WriteString("\n")
	for _, d := range cfg.ExcludedDirs {
		b.WriteString("  " + DimmedStyle.Render(d) + "\n")
	}
	b.WriteString("\n")

	// Default Session Name
	b.WriteString(SuccessStyle.Render("Default Session: "))
	b.WriteString(cfg.DefaultSessionName)
	b.WriteString("\n\n")

	// Container Timeout
	b.WriteString(SuccessStyle.Render("Container Timeout: "))
	b.WriteString(fmt.Sprintf("%ds", cfg.ContainerTimeout))
	b.WriteString("\n\n")

	b.WriteString(HelpStyle.Render("Press any key to return"))

	return b.String()
}
