// Package skill embeds gora8's Agent Skill (gora8-commerce) so the CLI
// can install it into a developer's own project without a network call
// or a checkout of the monorepo alongside the binary.
package skill

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed embedded/SKILL.md embedded/REFERENCE.md
var embedded embed.FS

// DirName is the directory this skill installs under, matching the
// directory-with-SKILL.md convention every Agent-Skill-aware host
// (Claude Code, and gora8-commerce/README.md's own install instructions)
// already expects.
const DirName = "gora8-commerce"

var fileNames = []string{"SKILL.md", "REFERENCE.md"}

// Files returns the embedded skill files, name -> content. The content
// is a build-time copy of gora8-commerce/{SKILL.md,REFERENCE.md} at the
// repo root (see embedded/sync.sh) — go:embed can't reach outside this
// module, so keeping the two in sync is a manual, documented step, not
// automatic.
func Files() (map[string][]byte, error) {
	out := make(map[string][]byte, len(fileNames))
	for _, name := range fileNames {
		data, err := embedded.ReadFile("embedded/" + name)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", name, err)
		}
		out[name] = data
	}
	return out, nil
}

// TargetDir returns where Install writes to for a given project directory.
func TargetDir(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "skills", DirName)
}

// AlreadyInstalled reports whether the skill is already present at its
// expected path, so callers that install opportunistically (init's
// auto-install) can skip rather than overwrite a developer's own edits.
func AlreadyInstalled(projectDir string) bool {
	_, err := os.Stat(filepath.Join(TargetDir(projectDir), "SKILL.md"))
	return err == nil
}

// Install writes SKILL.md and REFERENCE.md into
// <projectDir>/.claude/skills/gora8-commerce/, creating directories as
// needed, and unconditionally overwrites whatever is already there —
// callers that want to preserve local edits should check
// AlreadyInstalled first. Returns the directory written to.
func Install(projectDir string) (string, error) {
	files, err := Files()
	if err != nil {
		return "", err
	}
	target := TargetDir(projectDir)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", target, err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(target, name), data, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}
	return target, nil
}
