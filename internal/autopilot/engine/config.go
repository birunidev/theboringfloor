// Package engine wires the autopilot together: pick a card from a Space
// board, give it a worktree, run the pipeline through opencode, and act on
// the outcome (comment, move, commit, push, draft PR). A dry run only reads
// the board and runs pre-flight in a throwaway worktree.
package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Check is one deterministic command run in the card worktree. Argv is
// executed directly (no shell).
type Check struct {
	Name string   `json:"name"`
	Argv []string `json:"argv"`
}

// BoardConfig maps one Space board to one repository.
type BoardConfig struct {
	BoardID string `json:"board_id"`
	Name    string `json:"name"`
	// Repo
	RepoName string `json:"repo_name"`
	RepoDir  string `json:"repo_dir"`
	Base     string `json:"base"`
	// Lists, by name (resolved to ids at start)
	ReadyList   string   `json:"ready_list"`
	WorkingList string   `json:"working_list"`
	ReviewList  string   `json:"review_list"`
	BlockedList string   `json:"blocked_list"`
	ClarifyList string   `json:"clarify_list"` // falls back to BlockedList when missing on the board
	DoneList    string   `json:"done_list"`
	Label       string   `json:"label"`
	PolicyCards []string `json:"policy_cards"` // e.g. the READ FIRST objectives card
	Checks      []Check  `json:"checks"`
	// SandboxReadOnly are extra paths (venvs, dependency dirs) checks may read.
	SandboxReadOnly []string `json:"sandbox_read_only"`
	// Setup commands run in a fresh worktree before checks (e.g. symlink deps).
	Setup []Check `json:"setup"`
}

// Config is ~/.theboringfloor/autopilot/config.json.
type Config struct {
	Model          string        `json:"model"`
	PerCardTokens  int64         `json:"per_card_tokens"`
	DailyTokens    int64         `json:"daily_tokens"`
	RunTimeout     Duration      `json:"run_timeout"`
	Poll           Duration      `json:"poll"`
	MaxRounds      int           `json:"max_rounds"`
	ReadActions    []string      `json:"read_actions"`
	PatternActions []string      `json:"pattern_actions"`
	WriteActions   []string      `json:"write_actions"`
	BashAction     string        `json:"bash_action"`
	ExtraBash      []string      `json:"extra_bash"` // allowed command prefixes beyond the checks
	Boards         []BoardConfig `json:"boards"`
}

// Duration is a JSON "45m" style duration.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	d.Duration = v
	return err
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// Defaults fills unset fields.
func (c *Config) Defaults() {
	if c.Model == "" {
		c.Model = "opencode-go/muse-spark-1.3-contributor"
	}
	if c.PerCardTokens == 0 {
		c.PerCardTokens = 2_000_000
	}
	if c.DailyTokens == 0 {
		c.DailyTokens = 10_000_000
	}
	if c.RunTimeout.Duration == 0 {
		c.RunTimeout.Duration = 45 * time.Minute
	}
	if c.Poll.Duration == 0 {
		c.Poll.Duration = 10 * time.Minute
	}
	if c.MaxRounds == 0 {
		c.MaxRounds = 3
	}
	if len(c.ReadActions) == 0 {
		// Observed in opencode 2.2.5 logs: read (path resource).
		c.ReadActions = []string{"read", "list"}
	}
	if len(c.PatternActions) == 0 {
		// Observed: glob (pattern resource, e.g. "**/*").
		c.PatternActions = []string{"glob", "grep", "codesearch"}
	}
	if len(c.WriteActions) == 0 {
		c.WriteActions = []string{"edit", "write", "patch", "apply_patch", "multiedit"}
	}
	if c.BashAction == "" {
		c.BashAction = "shell" // observed in opencode 2.2.5 logs
	}
	for i := range c.Boards {
		b := &c.Boards[i]
		def := func(p *string, v string) {
			if *p == "" {
				*p = v
			}
		}
		def(&b.Base, "main")
		def(&b.ReadyList, "Todo")
		def(&b.ReviewList, "Review")
		def(&b.BlockedList, "On Hold")
		def(&b.ClarifyList, "Needs Clarification")
		def(&b.DoneList, "Done")
		def(&b.Label, "autopilot")
	}
}

// Validate refuses configs that would break a hard rule.
func (c Config) Validate() error {
	if len(c.Boards) == 0 {
		return errors.New("config: no boards")
	}
	for _, b := range c.Boards {
		if b.BoardID == "" || b.RepoName == "" || b.RepoDir == "" {
			return fmt.Errorf("config: board %q needs board_id, repo_name and repo_dir", b.Name)
		}
		if NoLabel(b.Label) && strings.EqualFold(strings.TrimSpace(b.ReadyList), "todo") {
			return fmt.Errorf("config: board %q selects every card in %q; use a dedicated queue list (e.g. \"Autopilot Queue\") when label is none", b.Name, b.ReadyList)
		}
		for _, l := range []string{b.ReadyList, b.WorkingList, b.ReviewList, b.BlockedList, b.ClarifyList} {
			if strings.EqualFold(strings.TrimSpace(l), strings.TrimSpace(b.DoneList)) {
				return fmt.Errorf("config: board %q routes cards into its Done list %q", b.Name, l)
			}
		}
	}
	return nil
}

// LoadConfig reads, defaults and validates path.
func LoadConfig(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	c.Defaults()
	return c, c.Validate()
}
