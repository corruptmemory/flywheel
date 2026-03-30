package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/flywheel/jail"
)

type agentJail struct {
	jail       *jail.UnshareJail
	scriptPath string
	fullCmd    string
}

// setupAgentJail creates a complete jail environment for one agent.
func setupAgentJail(
	tempBase string,
	agentName string,
	workspace string,
	mcpSocketDir string,
	promptContent string,
	currentUser *user.User,
) (*agentJail, error) {
	jailRoot := filepath.Join(tempBase, agentName, "root")
	configCopy := filepath.Join(tempBase, agentName, "claude-config")

	for _, d := range []string{jailRoot, configCopy} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Copy ~/.claude
	claudeDir := filepath.Join(currentUser.HomeDir, ".claude")
	claudeCopyDest := filepath.Join(configCopy, ".claude")
	cpCmd := exec.Command("cp", "-a", claudeDir, claudeCopyDest)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copy .claude: %s: %w", string(out), err)
	}
	exec.Command("chmod", "-R", "a+rwX", claudeCopyDest).Run()

	// Copy ~/.claude.json and inject workspace trust
	claudeJSON := filepath.Join(currentUser.HomeDir, ".claude.json")
	claudeJSONCopy := filepath.Join(configCopy, ".claude.json")
	hasClaudeJSON := false
	if _, err := os.Stat(claudeJSON); err == nil {
		hasClaudeJSON = true
		cpCmd2 := exec.Command("cp", "-a", claudeJSON, claudeJSONCopy)
		if out, err := cpCmd2.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("copy .claude.json: %s: %w", string(out), err)
		}
		os.Chmod(claudeJSONCopy, 0666)
		injectWorkspaceTrust(claudeJSONCopy)
	}

	// Write prompt
	os.MkdirAll(workspace, 0755)
	os.Chmod(workspace, 0777)
	if err := os.WriteFile(filepath.Join(workspace, ".flywheel-prompt.md"), []byte(promptContent), 0644); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}

	// Set up jail
	j := jail.NewUnshareJail(jailRoot)
	j.SetDropUser(currentUser.Username)

	homeInJail := filepath.Join(jailRoot, "home", currentUser.Username)
	workspaceInJail := filepath.Join(jailRoot, "workspace")
	mcpInJail := filepath.Join(jailRoot, "run", "mcp")
	tmpInJail := filepath.Join(jailRoot, "tmp")

	mounts := append(
		jail.HostMounts(jailRoot),
		jail.BindMount("/opt/claude-code", filepath.Join(jailRoot, "opt/claude-code"), true),
		jail.BindMount(claudeCopyDest, filepath.Join(homeInJail, ".claude"), false),
	)
	mounts = append(mounts,
		jail.BindMount(workspace, workspaceInJail, false),
		jail.TmpfsMount(tmpInJail),
		jail.BindMount(mcpSocketDir, mcpInJail, false),
	)

	if err := j.Setup(mounts); err != nil {
		return nil, fmt.Errorf("jail setup: %w", err)
	}

	// Create home dir and copy .claude.json
	os.MkdirAll(homeInJail, 0777)
	if hasClaudeJSON {
		exec.Command("cp", "-a", claudeJSONCopy, filepath.Join(homeInJail, ".claude.json")).Run()
	}

	// Generate jail script
	claudeInvocation := `/opt/claude-code/bin/claude --permission-mode bypassPermissions "Read .flywheel-prompt.md and follow ALL instructions exactly. Do not summarize or ask questions."`
	script := j.Script("/bin/bash", "-c", fmt.Sprintf("cd /workspace && exec %s", claudeInvocation))
	scriptPath := filepath.Join(tempBase, agentName, "jail-run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return nil, fmt.Errorf("write jail script: %w", err)
	}

	// Build tmux command
	unshareFlags := j.UnshareArgs()
	fullCmd := fmt.Sprintf("HOME=/home/%s TERM=xterm-256color DISABLE_AUTOUPDATER=1 unshare %s -- bash %s",
		currentUser.Username,
		strings.Join(unshareFlags, " "),
		scriptPath,
	)

	return &agentJail{jail: j, scriptPath: scriptPath, fullCmd: fullCmd}, nil
}

func injectWorkspaceTrust(claudeJSONPath string) {
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		return
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	projects, ok := cfg["projects"].(map[string]any)
	if !ok {
		projects = make(map[string]any)
		cfg["projects"] = projects
	}
	projects["/workspace"] = map[string]any{
		"allowedTools":           []any{},
		"hasTrustDialogAccepted": true,
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	os.WriteFile(claudeJSONPath, out, 0666)
}
