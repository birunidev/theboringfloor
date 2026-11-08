package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Profile is a named, explicit set of deterministic checks for one kind of
// card (e.g. core-frontend, core-backend, core-docs). A profile is chosen from
// the card title prefix, exactly like a lane: the match is configured by the
// member, never inferred from card prose and never by a model. A card that
// matches no profile, or more than one, is refused before any board write.
type Profile struct {
	Name          string   `json:"name"`
	TitlePrefixes []string `json:"title_prefixes"`
	Checks        []Check  `json:"checks"`
	// SandboxReadOnly are extra paths the profile's checks may read (a
	// venv, an nvm node install). Merged with the board's list.
	SandboxReadOnly []string `json:"sandbox_read_only,omitempty"`
	// RequireFiles must exist in the worktree before any check runs
	// (e.g. frontend/package-lock.json, because `npm ci` needs it).
	RequireFiles []string `json:"require_files,omitempty"`
	// RequireNPM lists package.json scripts that must exist and do real work.
	RequireNPM *NPMRequire `json:"require_npm,omitempty"`
}

// NPMRequire names package.json scripts a profile depends on.
type NPMRequire struct {
	Dir     string   `json:"dir"`
	Scripts []string `json:"scripts"`
}

var (
	// ErrNoProfile: profiles are configured but none matches the card title.
	ErrNoProfile = errors.New("no check profile matches the card title")
	// ErrAmbiguousProfile: more than one profile matches the card title.
	ErrAmbiguousProfile = errors.New("more than one check profile matches the card title")
)

var profileNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// ProfileFor returns the profile that governs a card. Boards without
// configured profiles keep the legacy board-wide checks; boards with profiles
// fail closed.
func (b BoardConfig) ProfileFor(title string) (Profile, error) {
	if len(b.Profiles) == 0 {
		return Profile{Name: "board-default", Checks: b.Checks, SandboxReadOnly: b.SandboxReadOnly}, nil
	}
	value := strings.ToLower(strings.TrimSpace(title))
	var matches []Profile
	for _, p := range b.Profiles {
		for _, prefix := range p.TitlePrefixes {
			if strings.HasPrefix(value, strings.ToLower(strings.TrimSpace(prefix))) {
				matches = append(matches, p)
				break
			}
		}
	}
	switch len(matches) {
	case 0:
		return Profile{}, ErrNoProfile
	case 1:
		p := matches[0]
		p.SandboxReadOnly = append(append([]string(nil), b.SandboxReadOnly...), p.SandboxReadOnly...)
		return p, nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return Profile{}, fmt.Errorf("%w: %s", ErrAmbiguousProfile, strings.Join(names, ", "))
	}
}

// Resolved returns the board config with this profile's checks and read-only
// paths applied, which is what the agent and checker must see for the card.
func (p Profile) Resolved(bc BoardConfig) BoardConfig {
	bc.Checks = p.Checks
	bc.SandboxReadOnly = p.SandboxReadOnly
	return bc
}

// Verify checks the profile's worktree preconditions. A returned error is
// reported as a failed check, so the agent can fix it (add the script, add the
// lockfile) instead of the run silently passing on nothing.
func (p Profile) Verify(worktree string) error {
	for _, rel := range p.RequireFiles {
		path, err := inside(worktree, rel)
		if err != nil {
			return err
		}
		fi, err := os.Lstat(path)
		if err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("required file %q is missing from the worktree", rel)
		}
	}
	if p.RequireNPM == nil {
		return nil
	}
	dir, err := inside(worktree, p.RequireNPM.Dir)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return fmt.Errorf("%s/package.json is unreadable: %w", p.RequireNPM.Dir, err)
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("%s/package.json is not valid JSON: %w", p.RequireNPM.Dir, err)
	}
	for _, name := range p.RequireNPM.Scripts {
		body, ok := pkg.Scripts[name]
		switch {
		case !ok:
			return fmt.Errorf("package.json has no %q script; the profile cannot verify without it", name)
		case trivialScript(body):
			return fmt.Errorf("package.json script %q (%q) does no real work", name, body)
		}
	}
	return nil
}

// trivialScript flags scripts that pass without verifying anything.
func trivialScript(body string) bool {
	s := strings.TrimSpace(body)
	if s == "" || s == "true" || s == ":" || strings.HasPrefix(s, "exit 0") || strings.HasPrefix(s, "echo") {
		return true
	}
	return strings.Contains(s, "|| true") || strings.Contains(s, "||true") || strings.Contains(s, "|| exit 0")
}

// inside resolves rel under root and refuses anything that escapes it.
func inside(root, rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be relative to the worktree", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the worktree", rel)
	}
	return filepath.Join(root, clean), nil
}

var forbiddenCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "fish": true, "ksh": true, "csh": true, "tcsh": true,
	"sudo": true, "su": true, "doas": true, "pkexec": true, "env": true, "xargs": true, "eval": true,
	"curl": true, "wget": true, "nc": true, "ncat": true, "ssh": true, "scp": true, "rsync": true,
	"rm": true, "dd": true, "mkfs": true, "chmod": true, "chown": true, "mount": true,
	"npx": true, "pnpx": true, "bunx": true,
}

// interpreterEval lists the flags that hand inline code to each interpreter.
// They are per interpreter because the same letter means something else
// elsewhere: `python -m pytest -p no:cacheprovider` is a pytest plugin flag.
var interpreterEval = map[string]map[string]bool{
	"python": {"-c": true},
	"node":   {"-e": true, "-p": true, "--eval": true, "--print": true},
	"perl":   {"-e": true, "-E": true},
	"ruby":   {"-e": true},
	"php":    {"-r": true},
}

// inlineCodeFlag returns the inline-code flag passed to an interpreter, if any.
// For Python everything after `-m <module>` belongs to the module, so scanning
// stops at -m; without -m every argument is scanned (a flag taking a value,
// like -W, cannot hide a following -c).
func inlineCodeFlag(base string, args []string) string {
	key := base
	if strings.HasPrefix(base, "python") {
		key = "python"
	}
	flags := interpreterEval[key]
	for _, a := range args {
		if a == "-m" && key == "python" {
			return ""
		}
		if flags[a] {
			return a
		}
	}
	return ""
}

// ValidateCheck refuses a check that could run arbitrary code or that
// weakens a hard rule. Argv is always executed directly (no shell), so this is
// a second layer, not the only one.
func ValidateCheck(ch Check) error {
	if strings.TrimSpace(ch.Name) == "" {
		return errors.New("check has no name")
	}
	if len(ch.Argv) == 0 || strings.TrimSpace(ch.Argv[0]) == "" {
		return fmt.Errorf("check %q has an empty argv", ch.Name)
	}
	for _, a := range ch.Argv {
		if strings.ContainsAny(a, "\x00\n\r") {
			return fmt.Errorf("check %q: argv contains a control character", ch.Name)
		}
	}
	base := filepath.Base(ch.Argv[0])
	if forbiddenCommands[base] {
		return fmt.Errorf("check %q runs forbidden command %q", ch.Name, base)
	}
	switch {
	case strings.HasPrefix(base, "python") || base == "node" || base == "perl" || base == "ruby" || base == "php":
		if flag := inlineCodeFlag(base, ch.Argv[1:]); flag != "" {
			return fmt.Errorf("check %q passes inline code to %s (%s); use a file or module", ch.Name, base, flag)
		}
	case base == "npm" || base == "yarn" || base == "pnpm" || base == "bun":
		for _, a := range ch.Argv[1:] {
			if a == "exec" || a == "x" || a == "dlx" || a == "create" {
				return fmt.Errorf("check %q runs downloaded code through %s %s", ch.Name, base, a)
			}
		}
	}
	if isPytest(ch.Argv) {
		for _, c := range ch.AllowExit {
			if c == pytestNoTests {
				return fmt.Errorf("check %q accepts pytest exit 5 (no tests collected); an empty suite is a failure", ch.Name)
			}
		}
	}
	if ch.Network && !isLockedNPMInstall(ch.Argv) {
		return fmt.Errorf("check %q requests network access; only `npm ci --ignore-scripts` may", ch.Name)
	}
	return nil
}

// isLockedNPMInstall is the single command allowed to reach the network: a
// lockfile-exact install with lifecycle scripts disabled.
func isLockedNPMInstall(argv []string) bool {
	if len(argv) < 3 || filepath.Base(argv[0]) != "npm" {
		return false
	}
	hasCI, noScripts := false, false
	for _, a := range argv[1:] {
		switch a {
		case "ci":
			hasCI = true
		case "--ignore-scripts":
			noScripts = true
		case "install", "i", "add", "update":
			return false
		}
	}
	return hasCI && noScripts
}

const pytestNoTests = 5

func isPytest(argv []string) bool {
	for i, a := range argv {
		b := filepath.Base(a)
		if b == "pytest" || b == "py.test" {
			return true
		}
		if a == "-m" && i+1 < len(argv) && (argv[i+1] == "pytest" || argv[i+1] == "py.test") {
			return true
		}
	}
	return false
}

// validateProfiles checks the profiles of one board.
func validateProfiles(b BoardConfig) error {
	if len(b.Profiles) == 0 {
		return nil
	}
	if len(b.Checks) > 0 {
		return fmt.Errorf("config: board %q has both board-wide checks and profiles; move the checks into profiles", b.Name)
	}
	seen := map[string]bool{}
	var prefixes []struct{ profile, value string }
	for _, p := range b.Profiles {
		if !profileNameRE.MatchString(p.Name) || seen[p.Name] {
			return fmt.Errorf("config: board %q has an invalid or duplicate profile name %q", b.Name, p.Name)
		}
		seen[p.Name] = true
		if len(p.TitlePrefixes) == 0 {
			return fmt.Errorf("config: profile %q needs explicit title_prefixes", p.Name)
		}
		if len(p.Checks) == 0 {
			return fmt.Errorf("config: profile %q has no checks; a profile that verifies nothing cannot pass a card", p.Name)
		}
		for _, raw := range p.TitlePrefixes {
			v := strings.ToLower(strings.TrimSpace(raw))
			if v == "" {
				return fmt.Errorf("config: profile %q has an empty title prefix", p.Name)
			}
			for _, prior := range prefixes {
				if prior.profile != p.Name && (strings.HasPrefix(v, prior.value) || strings.HasPrefix(prior.value, v)) {
					return fmt.Errorf("config: profile prefixes %q (%s) and %q (%s) overlap", prior.value, prior.profile, v, p.Name)
				}
			}
			prefixes = append(prefixes, struct{ profile, value string }{p.Name, v})
		}
		for _, ch := range p.Checks {
			if err := ValidateCheck(ch); err != nil {
				return fmt.Errorf("config: profile %q: %w", p.Name, err)
			}
		}
		for _, rel := range p.RequireFiles {
			if _, err := inside("/", rel); err != nil {
				return fmt.Errorf("config: profile %q: %w", p.Name, err)
			}
		}
		if p.RequireNPM != nil {
			if _, err := inside("/", p.RequireNPM.Dir); err != nil {
				return fmt.Errorf("config: profile %q: %w", p.Name, err)
			}
			if len(p.RequireNPM.Scripts) == 0 {
				return fmt.Errorf("config: profile %q require_npm lists no scripts", p.Name)
			}
		}
	}
	return nil
}
