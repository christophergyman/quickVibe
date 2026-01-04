// Package devcontainer provides functionality for managing devcontainers,
// git worktrees, and tmux sessions within devcontainers.
package devcontainer

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// Project represents a devcontainer project
type Project struct {
	Name string // Project directory name
	Path string // Full path to the project (workspace folder)
}

// WorktreeInfo represents a git worktree
type WorktreeInfo struct {
	Path     string // Worktree directory path
	Branch   string // Current branch name
	MainRepo string // Path to main repository
	GitDir   string // Path to worktree gitdir (.git/worktrees/<name>)
	IsMain   bool   // True if this is the main worktree
}

// newMainWorktreeInfo creates a WorktreeInfo for the main worktree of a repo
func newMainWorktreeInfo(path, branch string) WorktreeInfo {
	return WorktreeInfo{
		Path:     path,
		Branch:   branch,
		MainRepo: path,
		GitDir:   filepath.Join(path, ".git"),
		IsMain:   true,
	}
}

// newBranchWorktreeInfo creates a WorktreeInfo for a non-main worktree
func newBranchWorktreeInfo(path, branch, mainRepo string) WorktreeInfo {
	worktreeName := filepath.Base(path)
	return WorktreeInfo{
		Path:     path,
		Branch:   branch,
		MainRepo: mainRepo,
		GitDir:   filepath.Join(mainRepo, ".git", "worktrees", worktreeName),
		IsMain:   false,
	}
}

// ContainerStatus represents the runtime status of a devcontainer
type ContainerStatus string

const (
	StatusRunning ContainerStatus = "running"
	StatusStopped ContainerStatus = "stopped"
	StatusUnknown ContainerStatus = "unknown"
)

// ContainerInstance represents a specific devcontainer instance
// Each instance corresponds to a main repo or a git worktree
type ContainerInstance struct {
	Project              // Embedded: Name and Path (workspace folder)
	ConfigPath string    // Full path to devcontainer.json (from main repo)
	Worktree   *WorktreeInfo // Worktree info (nil for main repo if not a worktree)
}

// ContainerInstanceWithStatus extends ContainerInstance with runtime info
type ContainerInstanceWithStatus struct {
	ContainerInstance
	Status       ContainerStatus
	ContainerID  string
	SessionCount int
}

// DisplayName returns the formatted name for UI display
func (c ContainerInstance) DisplayName() string {
	if c.Worktree != nil && !c.Worktree.IsMain {
		return c.Name + " [" + c.Worktree.Branch + "]"
	}
	return c.Name
}

// devcontainerFoundFunc is a callback invoked when a devcontainer.json is found
// configPath is the full path to devcontainer.json
// projectPath is the root directory of the project
type devcontainerFoundFunc func(configPath, projectPath string)

// walkDevcontainerDirs walks through search paths looking for devcontainer.json files
// and invokes the callback for each one found
func walkDevcontainerDirs(searchPaths []string, maxDepth int, excludedDirs []string, onFound devcontainerFoundFunc) {
	// Build exclusion set for O(1) lookup
	excludeSet := make(map[string]bool, len(excludedDirs))
	for _, dir := range excludedDirs {
		excludeSet[dir] = true
	}

	for _, searchPath := range searchPaths {
		filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip directories we can't read
			}

			// Skip hidden directories (except .devcontainer)
			if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != ".devcontainer" {
				return fs.SkipDir
			}

			// Skip excluded directories
			if d.IsDir() && excludeSet[d.Name()] {
				return fs.SkipDir
			}

			// Check depth
			relPath, _ := filepath.Rel(searchPath, path)
			depth := strings.Count(relPath, string(os.PathSeparator))
			if depth > maxDepth {
				return fs.SkipDir
			}

			// Look for devcontainer.json
			if d.Name() == "devcontainer.json" {
				dir := filepath.Dir(path)

				// Only accept .devcontainer/devcontainer.json pattern
				if filepath.Base(dir) == ".devcontainer" {
					projectPath := filepath.Dir(dir)
					onFound(path, projectPath)
				}
			}

			return nil
		})
	}
}

// IsGitWorktree checks if the given path is a git worktree and returns its info
// Returns nil if the path is not a git worktree or not a git repository
func IsGitWorktree(path string) *WorktreeInfo {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return nil // Not a git repository
	}

	if info.IsDir() {
		// It's a regular git repository (main worktree)
		branch := getGitBranch(path)
		wt := newMainWorktreeInfo(path, branch)
		return &wt
	}

	// .git is a file - this is a worktree
	// Read the gitdir from the file
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return nil
	}

	// Parse: "gitdir: /path/to/.git/worktrees/name"
	line := strings.TrimSpace(string(content))
	if !strings.HasPrefix(line, "gitdir: ") {
		return nil
	}
	gitDir := strings.TrimPrefix(line, "gitdir: ")

	// Find the main repo (go up from .git/worktrees/<name> to .git, then parent)
	// gitDir is like /path/to/main/.git/worktrees/feature-x
	mainGitDir := filepath.Dir(filepath.Dir(gitDir)) // Go up to .git
	mainRepo := filepath.Dir(mainGitDir)             // Go up to main repo

	branch := getGitBranch(path)

	return &WorktreeInfo{
		Path:     path,
		Branch:   branch,
		MainRepo: mainRepo,
		GitDir:   gitDir,
		IsMain:   false,
	}
}

// getGitBranch returns the current branch name for a git repository
func getGitBranch(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// ListWorktrees returns all worktrees for a repository (including the main one)
func ListWorktrees(repoPath string) ([]WorktreeInfo, error) {
	// Run git worktree list --porcelain
	cmd := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %v", err)
	}

	var worktrees []WorktreeInfo
	var current WorktreeInfo
	isFirst := true

	// Helper to finalize and append the current worktree
	appendWorktree := func() {
		if current.Path == "" {
			return
		}
		if isFirst {
			worktrees = append(worktrees, newMainWorktreeInfo(current.Path, current.Branch))
		} else {
			worktrees = append(worktrees, newBranchWorktreeInfo(current.Path, current.Branch, repoPath))
		}
		isFirst = false
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			appendWorktree()
			current = WorktreeInfo{}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		} else if strings.HasPrefix(line, "HEAD ") {
			// Detached HEAD - use short SHA as branch name
			if current.Branch == "" {
				sha := strings.TrimPrefix(line, "HEAD ")
				if len(sha) > 7 {
					current.Branch = sha[:7]
				} else {
					current.Branch = sha
				}
			}
		}
	}

	// Handle last worktree if no trailing newline
	appendWorktree()

	return worktrees, nil
}

// GetMainRepo finds the main repository path from any worktree path
func GetMainRepo(worktreePath string) (string, error) {
	info := IsGitWorktree(worktreePath)
	if info == nil {
		return "", fmt.Errorf("not a git repository or worktree")
	}
	return info.MainRepo, nil
}

// DiscoverInstances finds all devcontainer instances in the given search paths
// For each project with a devcontainer.json, it finds all git worktrees
// and adds each worktree as a separate instance
func DiscoverInstances(searchPaths []string, maxDepth int, excludedDirs []string) []ContainerInstance {
	var instances []ContainerInstance
	seenProjects := make(map[string]bool)  // Track main repos we've processed
	seenWorktrees := make(map[string]bool) // Track worktree paths to deduplicate

	walkDevcontainerDirs(searchPaths, maxDepth, excludedDirs, func(configPath, projectPath string) {
		// Check if this is a git repo/worktree
		wtInfo := IsGitWorktree(projectPath)
		if wtInfo == nil {
			// Not a git repo - just add as a single instance without worktree info
			if !seenWorktrees[projectPath] {
				seenWorktrees[projectPath] = true
				instances = append(instances, ContainerInstance{
					Project: Project{
						Name: filepath.Base(projectPath),
						Path: projectPath,
					},
					ConfigPath: configPath,
					Worktree:   nil,
				})
			}
			return
		}

		// Get the main repo path
		mainRepo := wtInfo.MainRepo
		if seenProjects[mainRepo] {
			return // Already processed this project and its worktrees
		}
		seenProjects[mainRepo] = true

		// Find devcontainer.json in main repo (for worktrees to share)
		mainConfigPath := filepath.Join(mainRepo, ".devcontainer", "devcontainer.json")
		if _, err := os.Stat(mainConfigPath); err != nil {
			// Fall back to discovered config path
			mainConfigPath = configPath
		}

		// List all worktrees for this repository
		worktrees, err := ListWorktrees(mainRepo)
		if err != nil {
			// If we can't list worktrees, just add the discovered path
			if !seenWorktrees[projectPath] {
				seenWorktrees[projectPath] = true
				instances = append(instances, ContainerInstance{
					Project: Project{
						Name: filepath.Base(projectPath),
						Path: projectPath,
					},
					ConfigPath: mainConfigPath,
					Worktree:   wtInfo,
				})
			}
			return
		}

		// Add each worktree as a separate instance
		for _, wt := range worktrees {
			if seenWorktrees[wt.Path] {
				continue
			}
			seenWorktrees[wt.Path] = true

			// Copy worktree info
			wtCopy := wt
			instances = append(instances, ContainerInstance{
				Project: Project{
					Name: filepath.Base(mainRepo), // Use main repo name for all
					Path: wt.Path,
				},
				ConfigPath: mainConfigPath,
				Worktree:   &wtCopy,
			})
		}
	})

	return instances
}

// CheckCLI verifies the devcontainer CLI is installed
func CheckCLI() error {
	_, err := exec.LookPath("devcontainer")
	if err != nil {
		return fmt.Errorf("devcontainer CLI not found. Install with: npm install -g @devcontainers/cli")
	}
	return nil
}

// Up starts the devcontainer for a project
// Returns error if it fails
func Up(projectPath string) error {
	args := []string{"up", "--workspace-folder", projectPath}

	// For worktrees, mount the main repo's .git directory at the expected host path
	// This allows git to find the gitdir referenced in the worktree's .git file
	wtInfo := IsGitWorktree(projectPath)
	if wtInfo != nil && !wtInfo.IsMain {
		mainGitDir := filepath.Join(wtInfo.MainRepo, ".git")
		args = append(args, "--mount",
			fmt.Sprintf("type=bind,source=%s,target=%s", mainGitDir, mainGitDir))
	}

	cmd := exec.Command("devcontainer", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start container: %s", stderr.String())
	}
	return nil
}

// findContainerByPath finds a Docker container by its devcontainer.local_folder label
// If runningOnly is true, only searches running containers
// If runningOnly is false, searches all containers (including stopped)
func findContainerByPath(projectPath string, runningOnly bool) (string, error) {
	args := []string{"ps", "-q", "--filter", fmt.Sprintf("label=devcontainer.local_folder=%s", projectPath)}
	if !runningOnly {
		// Insert "-a" after "ps" to include stopped containers
		args = []string{"ps", "-a", "-q", "--filter", fmt.Sprintf("label=devcontainer.local_folder=%s", projectPath)}
	}
	cmd := exec.Command("docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to find container: %v", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// Stop stops the devcontainer by finding and stopping its Docker container
func Stop(projectPath string) error {
	containerID, err := findContainerByPath(projectPath, true)
	if err != nil {
		return err
	}
	if containerID == "" {
		return fmt.Errorf("no running container found for project")
	}
	stopCmd := exec.Command("docker", "stop", containerID)
	var stderr bytes.Buffer
	stopCmd.Stderr = &stderr
	if err := stopCmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %s", stderr.String())
	}
	return nil
}

// Restart restarts the devcontainer
func Restart(projectPath string) error {
	containerID, err := findContainerByPath(projectPath, false)
	if err != nil {
		return err
	}
	if containerID == "" {
		return Up(projectPath) // No container, just start
	}
	restartCmd := exec.Command("docker", "restart", containerID)
	var stderr bytes.Buffer
	restartCmd.Stderr = &stderr
	if err := restartCmd.Run(); err != nil {
		return fmt.Errorf("failed to restart container: %s", stderr.String())
	}
	return nil
}

// GetContainerStatus checks if a container is running for the given project path
func GetContainerStatus(projectPath string) (ContainerStatus, string) {
	// Check running containers first
	containerID, err := findContainerByPath(projectPath, true)
	if err != nil {
		return StatusUnknown, ""
	}
	if containerID != "" {
		return StatusRunning, containerID
	}

	// Check stopped containers
	cmd := exec.Command("docker", "ps", "-a", "-q",
		"--filter", fmt.Sprintf("label=devcontainer.local_folder=%s", projectPath),
		"--filter", "status=exited")
	output, err := cmd.Output()
	if err != nil {
		return StatusUnknown, ""
	}

	containerID = strings.TrimSpace(string(output))
	if containerID != "" {
		return StatusStopped, containerID
	}

	return StatusUnknown, ""
}

// GetAllInstancesStatus returns all instances with their current Docker status
func GetAllInstancesStatus(instances []ContainerInstance) []ContainerInstanceWithStatus {
	result := make([]ContainerInstanceWithStatus, len(instances))
	var wg sync.WaitGroup

	for i, inst := range instances {
		wg.Add(1)
		go func(idx int, instance ContainerInstance) {
			defer wg.Done()

			// Use path-based status check since each worktree has a unique path
			status, containerID := GetContainerStatus(instance.Path)
			sessionCount := 0

			// Only count sessions if container is running
			if status == StatusRunning {
				sessions, err := ListTmuxSessions(instance.Path)
				if err == nil {
					sessionCount = len(sessions)
				}
			}

			result[idx] = ContainerInstanceWithStatus{
				ContainerInstance: instance,
				Status:            status,
				ContainerID:       containerID,
				SessionCount:      sessionCount,
			}
		}(i, inst)
	}

	wg.Wait()
	return result
}

// ExecInteractive executes a command inside the devcontainer interactively
// This replaces the current process with the devcontainer exec
func ExecInteractive(projectPath string, args []string) error {
	devcontainerPath, err := exec.LookPath("devcontainer")
	if err != nil {
		return err
	}

	cmdArgs := []string{"devcontainer", "exec", "--workspace-folder", projectPath}
	cmdArgs = append(cmdArgs, args...)

	// Replace current process with devcontainer exec
	return syscall.Exec(devcontainerPath, cmdArgs, os.Environ())
}

// execInContainer runs a command inside the devcontainer and returns its output
func execInContainer(projectPath string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"exec", "--workspace-folder", projectPath}, args...)
	cmd := exec.Command("devcontainer", cmdArgs...)
	return cmd.Output()
}

// execInContainerWithStderr runs a command inside the devcontainer and captures stderr for errors
func execInContainerWithStderr(projectPath string, errPrefix string, args ...string) error {
	cmdArgs := append([]string{"exec", "--workspace-folder", projectPath}, args...)
	cmd := exec.Command("devcontainer", cmdArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %s", errPrefix, stderr.String())
	}
	return nil
}

// ListTmuxSessions lists tmux sessions inside the container
// Returns empty slice (not nil) if no sessions exist
func ListTmuxSessions(projectPath string) ([]string, error) {
	output, err := execInContainer(projectPath, "tmux", "list-sessions", "-F", "#{session_name}:#{session_attached}")
	if err != nil {
		// Exit code 1 means no sessions - return empty slice, not error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []string{}, nil
		}
		return nil, fmt.Errorf("ListTmuxSessions: %w", err)
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// CreateTmuxSession creates a new tmux session in the container
func CreateTmuxSession(projectPath, sessionName string) error {
	return execInContainerWithStderr(projectPath, "failed to create tmux session",
		"tmux", "new-session", "-d", "-s", sessionName)
}

// HasTmux checks if tmux is available in the container
func HasTmux(projectPath string) bool {
	_, err := execInContainer(projectPath, "which", "tmux")
	return err == nil
}

// KillTmuxSession kills a tmux session in the container
func KillTmuxSession(projectPath, sessionName string) error {
	return execInContainerWithStderr(projectPath, "failed to kill tmux session",
		"tmux", "kill-session", "-t", sessionName)
}

// CreateWorktree creates a new git worktree with a new branch
// Returns the path to the new worktree directory
func CreateWorktree(repoPath, branchName string) (string, error) {
	// Validate branch name
	if err := ValidateBranchName(branchName); err != nil {
		return "", err
	}

	// Check if this is a git repository
	wtInfo := IsGitWorktree(repoPath)
	if wtInfo == nil {
		return "", fmt.Errorf("not a git repository")
	}

	// Get the main repo path
	mainRepo := wtInfo.MainRepo

	// Create worktree path as sibling directory: repo-branchname
	repoName := filepath.Base(mainRepo)
	worktreePath := filepath.Join(filepath.Dir(mainRepo), repoName+"-"+branchName)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree directory already exists: %s", worktreePath)
	}

	// Create the worktree with a new branch
	cmd := exec.Command("git", "-C", mainRepo, "worktree", "add", "-b", branchName, worktreePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create worktree: %s", stderr.String())
	}

	return worktreePath, nil
}

// RemoveWorktree removes a git worktree
// If mainRepoPath is provided, it will be used when the worktree directory doesn't exist
func RemoveWorktree(worktreePath string, mainRepoPath ...string) error {
	// Stop any running Docker container for this worktree first
	// We attempt this regardless of whether the directory exists
	_ = Stop(worktreePath) // Ignore error - container may not be running

	wtInfo := IsGitWorktree(worktreePath)

	var mainRepo string
	if wtInfo != nil {
		// Directory exists and is a valid worktree
		if wtInfo.IsMain {
			return fmt.Errorf("cannot remove the main worktree")
		}
		mainRepo = wtInfo.MainRepo
	} else {
		// Directory doesn't exist or is not a valid worktree
		// Try to use provided mainRepoPath
		if len(mainRepoPath) > 0 && mainRepoPath[0] != "" {
			mainRepo = mainRepoPath[0]
		} else {
			return fmt.Errorf("not a git worktree")
		}
	}

	// Remove the worktree using --force flag to handle missing directories
	cmd := exec.Command("git", "-C", mainRepo, "worktree", "remove", "--force", worktreePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If removal failed, try pruning stale worktrees
		pruneCmd := exec.Command("git", "-C", mainRepo, "worktree", "prune")
		if pruneErr := pruneCmd.Run(); pruneErr == nil {
			// Pruning succeeded, check if the worktree was cleaned up
			return nil
		}
		return fmt.Errorf("failed to remove worktree: %s", stderr.String())
	}

	return nil
}

// ValidateBranchName checks if a branch name is valid for git
func ValidateBranchName(name string) error {
	if name == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if name == "main" || name == "master" {
		return fmt.Errorf("'%s' is a reserved branch name", name)
	}
	// Git branch name rules (simplified)
	if strings.HasPrefix(name, "-") {
		return fmt.Errorf("branch name cannot start with '-'")
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("branch name cannot start or end with '.'")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("branch name cannot contain '..'")
	}
	// Only allow alphanumeric, hyphen, underscore, slash for hierarchical branches
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '/') {
			return fmt.Errorf("branch name contains invalid character: %c", r)
		}
	}
	return nil
}
