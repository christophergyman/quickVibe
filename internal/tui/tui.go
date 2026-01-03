package tui

import (
	"os/exec"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/christophergyman/claude-quick/internal/config"
	"github.com/christophergyman/claude-quick/internal/devcontainer"
	"github.com/christophergyman/claude-quick/internal/tmux"
)

// State represents the current view state
type State int

const (
	StateDiscovering State = iota
	StateRefreshingStatus
	StateDashboard
	StateContainerStarting
	StateConfirmStop
	StateConfirmRestart
	StateContainerStopping
	StateContainerRestarting
	StateTmuxSelect
	StateNewSessionInput
	StateAttaching
	StateConfirmTmuxStop
	StateConfirmTmuxRestart
	StateTmuxStopping
	StateTmuxRestarting
	StateLoadingTmuxSessions
	StateError
	StateShowConfig
)

// Model is the main Bubbletea model
type Model struct {
	state           State
	projects        []devcontainer.Project
	projectsStatus  []devcontainer.ProjectWithStatus
	selectedProject *devcontainer.Project
	tmuxSessions    []tmux.Session
	selectedSession *tmux.Session
	cursor          int
	spinner         spinner.Model
	textInput       textinput.Model
	err             error
	errHint         string
	width           int
	height          int
	config          *config.Config
	previousState   State
}

// Messages for async operations
type projectsDiscoveredMsg struct {
	projects []devcontainer.Project
}
type containerStatusRefreshedMsg struct {
	statuses []devcontainer.ProjectWithStatus
}
type containerStartedMsg struct{}
type containerErrorMsg struct{ err error }
type tmuxSessionsLoadedMsg struct{ sessions []string }
type tmuxSessionCreatedMsg struct{}
type containerStoppedMsg struct{}
type containerRestartedMsg struct{}
type tmuxSessionStoppedMsg struct{}
type tmuxSessionRestartedMsg struct{}
type tmuxDetachedMsg struct{}

// New creates a new Model with discovered projects
func New(projects []devcontainer.Project, cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = SpinnerStyle

	ti := textinput.New()
	ti.Placeholder = cfg.DefaultSessionName
	ti.CharLimit = 50
	ti.Width = 30

	return Model{
		state:     StateDashboard,
		projects:  projects,
		spinner:   s,
		textInput: ti,
		config:    cfg,
	}
}

// NewWithDiscovery creates a Model that will discover projects asynchronously
func NewWithDiscovery(cfg *config.Config) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = SpinnerStyle

	ti := textinput.New()
	ti.Placeholder = cfg.DefaultSessionName
	ti.CharLimit = 50
	ti.Width = 30

	return Model{
		state:     StateDiscovering,
		projects:  nil,
		spinner:   s,
		textInput: ti,
		config:    cfg,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	if m.state == StateDiscovering {
		return tea.Batch(m.spinner.Tick, m.discoverProjects())
	}
	return nil
}

// discoverProjects returns a command that discovers devcontainer projects
func (m Model) discoverProjects() tea.Cmd {
	return func() tea.Msg {
		projects := devcontainer.Discover(
			m.config.SearchPaths,
			m.config.MaxDepth,
			m.config.ExcludedDirs,
		)
		return projectsDiscoveredMsg{projects: projects}
	}
}

// refreshProjectStatus returns a command that refreshes container status for all projects
func (m Model) refreshProjectStatus() tea.Cmd {
	return func() tea.Msg {
		statuses := devcontainer.GetAllProjectsStatus(m.projects)
		return containerStatusRefreshedMsg{statuses: statuses}
	}
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case projectsDiscoveredMsg:
		m.projects = msg.projects
		m.state = StateRefreshingStatus
		m.cursor = 0
		return m, tea.Batch(m.spinner.Tick, m.refreshProjectStatus())

	case containerStatusRefreshedMsg:
		m.projectsStatus = msg.statuses
		m.state = StateDashboard
		return m, nil

	case tmuxDetachedMsg:
		// User detached from tmux, return to dashboard with status refresh
		m.state = StateRefreshingStatus
		m.selectedProject = nil
		m.cursor = 0
		return m, tea.Batch(m.spinner.Tick, m.refreshProjectStatus())

	case containerStartedMsg:
		return m.handleContainerStarted()

	case containerErrorMsg:
		m.state = StateError
		m.err = msg.err
		m.errHint = "Press any key to go back"
		return m, nil

	case tmuxSessionsLoadedMsg:
		m.tmuxSessions = tmux.ParseSessions(msg.sessions)
		m.state = StateTmuxSelect
		m.cursor = 0
		return m, nil

	case tmuxSessionCreatedMsg:
		// Session created, now attach
		sessionName := m.textInput.Value()
		return m.attachToSession(sessionName)

	case containerStoppedMsg, containerRestartedMsg:
		// Refresh status after container operation
		m.state = StateRefreshingStatus
		m.selectedProject = nil
		return m, tea.Batch(m.spinner.Tick, m.refreshProjectStatus())

	case tmuxSessionStoppedMsg, tmuxSessionRestartedMsg:
		// Reload tmux sessions after stop/restart with loading animation
		m.selectedSession = nil
		m.cursor = 0
		m.state = StateLoadingTmuxSessions
		return m, tea.Batch(m.spinner.Tick, m.loadTmuxSessions())

	}

	// Update text input if in input state
	if m.state == StateNewSessionInput {
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleKeyPress processes keyboard input based on current state
func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case StateDashboard:
		return m.handleDashboardKey(msg)
	case StateConfirmStop, StateConfirmRestart:
		return m.handleConfirmKey(msg)
	case StateConfirmTmuxStop, StateConfirmTmuxRestart:
		return m.handleTmuxConfirmKey(msg)
	case StateTmuxSelect:
		return m.handleTmuxSelectKey(msg)
	case StateNewSessionInput:
		return m.handleNewSessionInputKey(msg)
	case StateError:
		// Any key returns to container select
		m.state = StateDashboard
		m.err = nil
		return m, nil
	case StateShowConfig:
		// Any key returns to previous state
		m.state = m.previousState
		return m, nil
	}
	return m, nil
}

func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.projectsStatus)-1 {
			m.cursor++
		}

	case "enter":
		if len(m.projectsStatus) > 0 {
			m.selectedProject = &m.projectsStatus[m.cursor].Project
			if m.projectsStatus[m.cursor].Status == devcontainer.StatusRunning {
				// Container is running, load tmux sessions
				m.state = StateLoadingTmuxSessions
				return m, tea.Batch(m.spinner.Tick, m.loadTmuxSessions())
			}
			// Container is stopped or unknown, start it
			m.state = StateContainerStarting
			return m, tea.Batch(
				m.spinner.Tick,
				m.startContainer(),
			)
		}

	case "x":
		if len(m.projectsStatus) > 0 {
			m.selectedProject = &m.projectsStatus[m.cursor].Project
			m.state = StateConfirmStop
		}

	case "r":
		if len(m.projectsStatus) > 0 {
			m.selectedProject = &m.projectsStatus[m.cursor].Project
			m.state = StateConfirmRestart
		}

	case "R":
		// Manual refresh
		m.state = StateRefreshingStatus
		return m, tea.Batch(m.spinner.Tick, m.refreshProjectStatus())

	case "?":
		m.previousState = m.state
		m.state = StateShowConfig
		return m, nil
	}
	return m, nil
}

func (m Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.state == StateConfirmStop {
			m.state = StateContainerStopping
			return m, tea.Batch(m.spinner.Tick, m.stopContainer())
		}
		m.state = StateContainerRestarting
		return m, tea.Batch(m.spinner.Tick, m.restartContainer())
	case "n", "N", "esc":
		m.state = StateDashboard
		m.selectedProject = nil
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleTmuxConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.state == StateConfirmTmuxStop {
			m.state = StateTmuxStopping
			return m, tea.Batch(m.spinner.Tick, m.stopTmuxSession())
		}
		m.state = StateTmuxRestarting
		return m, tea.Batch(m.spinner.Tick, m.restartTmuxSession())
	case "n", "N", "esc":
		m.state = StateTmuxSelect
		m.selectedSession = nil
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleTmuxSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalOptions := TotalTmuxOptions(m.tmuxSessions)

	switch msg.String() {
	case "q", "esc":
		// Go back to container select with status refresh
		m.state = StateRefreshingStatus
		m.cursor = 0
		m.selectedProject = nil
		return m, tea.Batch(m.spinner.Tick, m.refreshProjectStatus())

	case "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < totalOptions-1 {
			m.cursor++
		}

	case "x":
		// Stop/kill selected tmux session (only for existing sessions)
		if m.cursor < len(m.tmuxSessions) {
			m.selectedSession = &m.tmuxSessions[m.cursor]
			m.state = StateConfirmTmuxStop
		}

	case "r":
		// Restart selected tmux session (only for existing sessions)
		if m.cursor < len(m.tmuxSessions) {
			m.selectedSession = &m.tmuxSessions[m.cursor]
			m.state = StateConfirmTmuxRestart
		}

	case "?":
		m.previousState = m.state
		m.state = StateShowConfig
		return m, nil

	case "enter":
		if IsNewSessionSelected(m.tmuxSessions, m.cursor) {
			// Show text input for new session name
			m.state = StateNewSessionInput
			m.textInput.SetValue("")
			m.textInput.Focus()
			return m, textinput.Blink
		}
		// Attach to existing session
		if m.cursor < len(m.tmuxSessions) {
			return m.attachToSession(m.tmuxSessions[m.cursor].Name)
		}
	}
	return m, nil
}

func (m Model) handleNewSessionInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Cancel and go back to tmux select
		m.state = StateTmuxSelect
		return m, nil

	case "ctrl+c":
		return m, tea.Quit

	case "enter":
		name := m.textInput.Value()
		if name == "" {
			name = m.config.DefaultSessionName
		}
		m.state = StateAttaching
		return m, tea.Batch(
			m.spinner.Tick,
			m.createTmuxSession(name),
		)
	}

	// Pass other keys to text input
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// startContainer returns a command that starts the devcontainer
func (m Model) startContainer() tea.Cmd {
	return func() tea.Msg {
		if m.selectedProject == nil {
			return containerErrorMsg{err: nil}
		}

		// Check if devcontainer CLI is available
		if err := devcontainer.CheckCLI(); err != nil {
			return containerErrorMsg{err: err}
		}

		// Start the container
		if err := devcontainer.Up(m.selectedProject.Path); err != nil {
			return containerErrorMsg{err: err}
		}

		// Check if tmux is available in container
		if !devcontainer.HasTmux(m.selectedProject.Path) {
			return containerErrorMsg{err: &tmuxNotFoundError{}}
		}

		return containerStartedMsg{}
	}
}

// stopContainer returns a command that stops the devcontainer
func (m Model) stopContainer() tea.Cmd {
	return func() tea.Msg {
		if m.selectedProject == nil {
			return containerErrorMsg{err: nil}
		}
		if err := devcontainer.Stop(m.selectedProject.Path); err != nil {
			return containerErrorMsg{err: err}
		}
		return containerStoppedMsg{}
	}
}

// restartContainer returns a command that restarts the devcontainer
func (m Model) restartContainer() tea.Cmd {
	return func() tea.Msg {
		if m.selectedProject == nil {
			return containerErrorMsg{err: nil}
		}
		if err := devcontainer.Restart(m.selectedProject.Path); err != nil {
			return containerErrorMsg{err: err}
		}
		return containerRestartedMsg{}
	}
}

// stopTmuxSession returns a command that stops/kills a tmux session
func (m Model) stopTmuxSession() tea.Cmd {
	return func() tea.Msg {
		if m.selectedProject == nil || m.selectedSession == nil {
			return containerErrorMsg{err: nil}
		}
		if err := devcontainer.KillTmuxSession(m.selectedProject.Path, m.selectedSession.Name); err != nil {
			return containerErrorMsg{err: err}
		}
		return tmuxSessionStoppedMsg{}
	}
}

// restartTmuxSession returns a command that restarts a tmux session (kill + create)
func (m Model) restartTmuxSession() tea.Cmd {
	return func() tea.Msg {
		if m.selectedProject == nil || m.selectedSession == nil {
			return containerErrorMsg{err: nil}
		}
		sessionName := m.selectedSession.Name
		// Kill existing session
		if err := devcontainer.KillTmuxSession(m.selectedProject.Path, sessionName); err != nil {
			return containerErrorMsg{err: err}
		}
		// Create new session with same name
		if err := devcontainer.CreateTmuxSession(m.selectedProject.Path, sessionName); err != nil {
			return containerErrorMsg{err: err}
		}
		return tmuxSessionRestartedMsg{}
	}
}

// handleContainerStarted is called when container has started
func (m Model) handleContainerStarted() (tea.Model, tea.Cmd) {
	// Transition to loading state with spinner
	m.state = StateLoadingTmuxSessions
	return m, tea.Batch(m.spinner.Tick, m.loadTmuxSessions())
}

// loadTmuxSessions returns a command that loads tmux sessions
func (m Model) loadTmuxSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := devcontainer.ListTmuxSessions(m.selectedProject.Path)
		if err != nil {
			return containerErrorMsg{err: err}
		}
		return tmuxSessionsLoadedMsg{sessions: sessions}
	}
}

// createTmuxSession creates a new tmux session in the container
func (m Model) createTmuxSession(name string) tea.Cmd {
	return func() tea.Msg {
		if err := devcontainer.CreateTmuxSession(m.selectedProject.Path, name); err != nil {
			return containerErrorMsg{err: err}
		}
		return tmuxSessionCreatedMsg{}
	}
}

// attachToSession attaches to a tmux session using tea.ExecProcess
// This suspends the TUI, runs tmux as a subprocess, and returns to TUI on detach
func (m Model) attachToSession(sessionName string) (tea.Model, tea.Cmd) {
	m.state = StateAttaching

	// Build the command to attach to tmux
	c := exec.Command("devcontainer", "exec", "--workspace-folder",
		m.selectedProject.Path, "tmux", "attach", "-t", sessionName)

	// Use tea.ExecProcess to run tmux and return to TUI when done
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		// This is called when tmux detaches or exits
		return tmuxDetachedMsg{}
	})
}

// View implements tea.Model
func (m Model) View() string {
	switch m.state {
	case StateDiscovering:
		return RenderDiscovering(m.spinner.View())

	case StateRefreshingStatus:
		return RenderRefreshingStatus(m.spinner.View())

	case StateDashboard:
		return RenderDashboard(m.projectsStatus, m.cursor, m.width)

	case StateContainerStarting:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderContainerStarting(projectName, m.spinner.View())

	case StateConfirmStop:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderConfirmDialog("stop", projectName)

	case StateConfirmRestart:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderConfirmDialog("restart", projectName)

	case StateContainerStopping:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderContainerOperation("Stopping", projectName, m.spinner.View())

	case StateContainerRestarting:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderContainerOperation("Restarting", projectName, m.spinner.View())

	case StateConfirmTmuxStop:
		sessionName := ""
		if m.selectedSession != nil {
			sessionName = m.selectedSession.Name
		}
		return RenderTmuxConfirmDialog("stop", sessionName)

	case StateConfirmTmuxRestart:
		sessionName := ""
		if m.selectedSession != nil {
			sessionName = m.selectedSession.Name
		}
		return RenderTmuxConfirmDialog("restart", sessionName)

	case StateTmuxStopping:
		sessionName := ""
		if m.selectedSession != nil {
			sessionName = m.selectedSession.Name
		}
		return RenderTmuxOperation("Stopping", sessionName, m.spinner.View())

	case StateTmuxRestarting:
		sessionName := ""
		if m.selectedSession != nil {
			sessionName = m.selectedSession.Name
		}
		return RenderTmuxOperation("Restarting", sessionName, m.spinner.View())

	case StateLoadingTmuxSessions:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderLoadingTmuxSessions(projectName, m.spinner.View())

	case StateTmuxSelect:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderTmuxSelect(projectName, m.tmuxSessions, m.cursor)

	case StateNewSessionInput:
		projectName := ""
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		return RenderNewSessionInput(projectName, m.textInput)

	case StateAttaching:
		projectName := ""
		sessionName := m.textInput.Value()
		if m.selectedProject != nil {
			projectName = m.selectedProject.Name
		}
		if m.cursor < len(m.tmuxSessions) {
			sessionName = m.tmuxSessions[m.cursor].Name
		}
		return RenderAttaching(projectName, sessionName, m.spinner.View())

	case StateError:
		return RenderError(m.err, m.errHint)

	case StateShowConfig:
		return RenderConfigDisplay(m.config)
	}

	return ""
}

// tmuxNotFoundError indicates tmux is not available in the container
type tmuxNotFoundError struct{}

func (e *tmuxNotFoundError) Error() string {
	return "tmux not found in container. Please install tmux in your devcontainer."
}
