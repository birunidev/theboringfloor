package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/theboringhumane/theboringfloor/internal/autopilot/pipeline"
)

// Runner-owned npm lockfile generation.
//
// A frontend toolchain card edits package.json but has no way to produce a real
// package-lock.json: the implementer agent has no network and no npm (and
// must never be given an `npm install` command prefix, which would accept
// arbitrary flags and package sources). The runner therefore generates the
// lockfile itself, as a check step, from the card's package.json:
//
//	npm [--prefix <dir>] install --package-lock-only --ignore-scripts
//
// It runs in the same bubblewrap sandbox as every check (empty HOME, cleared
// environment, private cache, registry pinned to registry.npmjs.org), with
// the network shared for that single command. Before it runs, the runner
// refuses anything that could redirect the install; after it runs, the runner
// validates what npm produced. This is not an egress firewall: see
// docs/autopilot-check-profiles.md for the exact boundary.

const (
	maxPackageJSONBytes = 256 << 10
	maxLockfileBytes    = 8 << 20
	maxDeclaredDeps     = 400
)

// LockGenNPM recognises the single argv allowed to generate a lockfile and
// returns the project directory. The argv must be exactly
// `npm install --package-lock-only --ignore-scripts` or
// `npm --prefix <relative dir> install --package-lock-only --ignore-scripts`.
// Any other flag or argument (a package name, --registry, --save, --global,
// --workspaces, --force, --before, an absolute or .. prefix) is refused.
func LockGenNPM(argv []string) (dir string, ok bool) {
	if len(argv) == 0 || argv[0] != "npm" {
		return "", false
	}
	rest := argv[1:]
	dir = "."
	if len(rest) >= 2 && rest[0] == "--prefix" {
		dir, rest = rest[1], rest[2:]
		if strings.HasPrefix(dir, "-") || dir == "" {
			return "", false
		}
		if _, err := inside("/", dir); err != nil {
			return "", false
		}
	}
	if len(rest) != 3 || rest[0] != "install" || rest[1] != "--package-lock-only" || rest[2] != "--ignore-scripts" {
		return "", false
	}
	return dir, true
}

// refuseNPMConfigFiles refuses worktrees holding npm configuration that npm
// would read and that an environment variable cannot override: a project
// .npmrc (scoped registries, ignore-scripts=false) or an npm-shrinkwrap.json,
// which npm prefers over package-lock.json.
func refuseNPMConfigFiles(worktree, realDir string) error {
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return err
	}
	for _, d := range []string{root, realDir} {
		for _, name := range []string{".npmrc", ".yarnrc", ".yarnrc.yml", ".pnpmrc"} {
			if _, err := os.Lstat(filepath.Join(d, name)); err == nil {
				return fmt.Errorf("%s exists in the worktree; npm configuration there could redirect the registry", name)
			}
		}
		if _, err := os.Lstat(filepath.Join(d, "npm-shrinkwrap.json")); err == nil {
			return errors.New("npm-shrinkwrap.json exists in the worktree; npm would prefer it over package-lock.json")
		}
	}
	return nil
}

// checkLockfileJSON validates a package-lock.json: version 2 or 3, no linked
// packages, and every package fetched from the pinned registry over https with
// an integrity hash (bundled dependencies only under a validated parent).
func checkLockfileJSON(data []byte) error {
	var doc struct {
		LockfileVersion int `json:"lockfileVersion"`
		Packages        map[string]struct {
			Resolved  string `json:"resolved"`
			Integrity string `json:"integrity"`
			Link      bool   `json:"link"`
			InBundle  bool   `json:"inBundle"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("package-lock.json is not valid JSON: %w", err)
	}
	if (doc.LockfileVersion != 2 && doc.LockfileVersion != 3) || len(doc.Packages) == 0 {
		return errors.New("package-lock.json needs lockfileVersion 2 or 3 with a packages map")
	}
	valid := func(resolved, integrity string) bool {
		return strings.HasPrefix(resolved, lockHostAllowed) && (strings.HasPrefix(integrity, "sha512-") || strings.HasPrefix(integrity, "sha384-"))
	}
	for name, pkg := range doc.Packages {
		if name == "" {
			continue
		}
		if pkg.Link {
			return fmt.Errorf("lockfile entry %q is a link; linked packages are not allowed", name)
		}
		if pkg.Resolved == "" && pkg.InBundle {
			// A bundled dependency ships inside its parent's tarball and has no
			// URL of its own. Accept it only when an ancestor entry that is
			// itself fetched (or bundled) from the pinned registry carries it.
			if err := bundledUnderValidParent(doc.Packages, name, valid); err != nil {
				return fmt.Errorf("lockfile entry %q: %w", name, err)
			}
			continue
		}
		if !strings.HasPrefix(pkg.Resolved, lockHostAllowed) {
			return fmt.Errorf("lockfile entry %q resolves from %q, not %s", name, clipLock(pkg.Resolved), lockHostAllowed)
		}
		if !valid(pkg.Resolved, pkg.Integrity) {
			return fmt.Errorf("lockfile entry %q has no sha512/sha384 integrity", name)
		}
	}
	return nil
}

var (
	npmNameRE = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)
	// npmSpecRE is the version-range alphabet only. It has no ':', '/', '\\' or
	// '@', so git URLs, github:user/repo, https tarballs, file:, link:,
	// workspace:, npm: aliases and relative paths cannot match.
	npmSpecRE   = regexp.MustCompile(`^[0-9A-Za-z.*~^<>=| +-]{1,64}$`)
	npmDigitRE  = regexp.MustCompile(`[0-9]`)
	beforeStamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$`)
)

// validateDepSpec accepts only a registry version range. A bare dist-tag
// ("latest") or "*" has no digit and is refused: the card must pin versions.
func validateDepSpec(name, spec string) error {
	if len(name) > 214 || !npmNameRE.MatchString(name) {
		return fmt.Errorf("dependency name %q is not a plain registry package name", clipLock(name))
	}
	if !npmSpecRE.MatchString(spec) {
		return fmt.Errorf("dependency %q has version spec %q, which is not a plain registry version range (git, url, file, link, workspace, alias and path sources are refused)", name, clipLock(spec))
	}
	if !npmDigitRE.MatchString(spec) {
		return fmt.Errorf("dependency %q has version spec %q; pin a version", name, spec)
	}
	return nil
}

// validatePackageJSON refuses a package.json that could make npm fetch from
// anywhere but the pinned registry, or that declares workspaces.
func validatePackageJSON(data []byte) error {
	if len(data) > maxPackageJSONBytes {
		return fmt.Errorf("package.json is larger than %d bytes", maxPackageJSONBytes)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return fmt.Errorf("package.json is not valid JSON: %w", err)
	}
	if _, ok := top["workspaces"]; ok {
		return errors.New("package.json declares workspaces; linked or nested packages are not allowed")
	}
	count := 0
	for _, field := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		raw, ok := top[field]
		if !ok {
			continue
		}
		var deps map[string]json.RawMessage
		if err := json.Unmarshal(raw, &deps); err != nil {
			return fmt.Errorf("package.json %s is not an object", field)
		}
		for name, v := range deps {
			var spec string
			if err := json.Unmarshal(v, &spec); err != nil {
				return fmt.Errorf("package.json %s.%s is not a string", field, name)
			}
			if err := validateDepSpec(name, spec); err != nil {
				return fmt.Errorf("package.json %s: %w", field, err)
			}
			count++
		}
	}
	for _, field := range []string{"overrides", "resolutions"} {
		if raw, ok := top[field]; ok {
			n, err := validateOverrides(field, raw, 0)
			if err != nil {
				return err
			}
			count += n
		}
	}
	if count > maxDeclaredDeps {
		return fmt.Errorf("package.json declares %d dependency entries; the limit is %d", count, maxDeclaredDeps)
	}
	return nil
}

// validateOverrides walks an overrides/resolutions object. Its keys may carry a
// version range after '@' (for example "foo@^1"); every leaf must be a plain
// registry range.
func validateOverrides(field string, raw json.RawMessage, depth int) (int, error) {
	if depth > 4 {
		return 0, fmt.Errorf("package.json %s is nested too deeply", field)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, fmt.Errorf("package.json %s is not an object", field)
	}
	n := 0
	for key, v := range m {
		name := key
		if i := strings.LastIndex(key, "@"); i > 0 {
			name = key[:i]
			if !npmSpecRE.MatchString(key[i+1:]) {
				return 0, fmt.Errorf("package.json %s key %q carries a version that is not a plain range", field, clipLock(key))
			}
		}
		if !npmNameRE.MatchString(name) {
			return 0, fmt.Errorf("package.json %s key %q is not a plain registry package name", field, clipLock(key))
		}
		var spec string
		if json.Unmarshal(v, &spec) == nil {
			if err := validateDepSpec(name, spec); err != nil {
				return 0, fmt.Errorf("package.json %s: %w", field, err)
			}
			n++
			continue
		}
		sub, err := validateOverrides(field, v, depth+1)
		if err != nil {
			return 0, err
		}
		n += sub
	}
	return n, nil
}

// lockGenPlan is what prepareLockGeneration approved: real paths inside the
// worktree and the package.json it validated.
type lockGenPlan struct {
	dir, pkgPath, lockPath string
	pkgSHA                 string
}

func fileSHA(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// regularFile returns the real path of a regular, non-symlink file inside the
// worktree, or an error saying why not.
func regularFile(worktree, p string) (string, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", errors.New("is not a regular file (a symlink or directory is refused)")
	}
	return realInside(worktree, p)
}

// prepareLockGeneration is every check that runs before npm is started. Nothing
// is executed and nothing is fetched if any of it fails.
func prepareLockGeneration(worktree string, argv []string) (lockGenPlan, error) {
	rel, ok := LockGenNPM(argv)
	if !ok {
		return lockGenPlan{}, errors.New("argv is not the allowlisted `npm [--prefix <dir>] install --package-lock-only --ignore-scripts`")
	}
	dir, err := realInside(worktree, filepath.Join(worktree, rel))
	if err != nil {
		return lockGenPlan{}, fmt.Errorf("project directory: %w", err)
	}
	if err := refuseNPMConfigFiles(worktree, dir); err != nil {
		return lockGenPlan{}, err
	}
	pkg, err := regularFile(worktree, filepath.Join(dir, "package.json"))
	if err != nil {
		return lockGenPlan{}, fmt.Errorf("package.json: %w", err)
	}
	data, err := os.ReadFile(pkg)
	if err != nil {
		return lockGenPlan{}, fmt.Errorf("package.json is unreadable: %w", err)
	}
	if err := validatePackageJSON(data); err != nil {
		return lockGenPlan{}, err
	}
	lock := filepath.Join(dir, "package-lock.json")
	if _, err := os.Lstat(lock); err == nil {
		if _, err := regularFile(worktree, lock); err != nil {
			return lockGenPlan{}, fmt.Errorf("existing package-lock.json: %w", err)
		}
	}
	return lockGenPlan{dir: dir, pkgPath: pkg, lockPath: lock, pkgSHA: fileSHA(data)}, nil
}

// clearInstalledTree removes the project's node_modules before generation.
// With a node_modules tree present, `npm install --package-lock-only` builds
// lockfile entries from the installed tree, which carries no registry
// metadata, so the entries come out without "resolved" and integrity. A
// second fix round, or a retried card, reuses the worktree and its
// node_modules from the previous `npm ci`, so without this the regenerated
// lockfile is incomplete. The tree is derived, ignored by git, and removed by
// `npm ci` anyway. A symlinked node_modules is refused, never followed.
func (p lockGenPlan) clearInstalledTree() error {
	nm := filepath.Join(p.dir, "node_modules")
	fi, err := os.Lstat(nm)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return errors.New("node_modules is a symlink or not a directory (refused, not followed)")
	}
	return os.RemoveAll(nm)
}

// verify is every check that runs after npm: package.json must be byte-for-byte
// what was validated, and the lockfile npm produced must be a regular file that
// passes the same validation `npm ci` gets. It returns the lockfile hash.
func (p lockGenPlan) verify(worktree string) (sha string, entries int, err error) {
	cur, err := os.ReadFile(p.pkgPath)
	if err != nil {
		return "", 0, fmt.Errorf("package.json is unreadable after generation: %w", err)
	}
	if fileSHA(cur) != p.pkgSHA {
		return "", 0, errors.New("npm changed package.json; generation must not edit it")
	}
	real, err := regularFile(worktree, p.lockPath)
	if err != nil {
		return "", 0, fmt.Errorf("package-lock.json was not produced as a regular file: %w", err)
	}
	fi, err := os.Stat(real)
	if err != nil || fi.Size() == 0 {
		return "", 0, errors.New("package-lock.json is empty")
	}
	if fi.Size() > maxLockfileBytes {
		return "", 0, fmt.Errorf("package-lock.json is larger than %d bytes", maxLockfileBytes)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return "", 0, err
	}
	if err := checkLockfileJSON(data); err != nil {
		return "", 0, err
	}
	var doc struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	_ = json.Unmarshal(data, &doc)
	return fileSHA(data), len(doc.Packages), nil
}

// runLockGeneration executes the runner-owned lockfile step. It is called only
// for the exact argv LockGenNPM accepts, and only when the profile enabled it.
func (c CmdChecker) runLockGeneration(ctx context.Context, ch Check, to time.Duration) (pipeline.CheckResult, error) {
	res := pipeline.CheckResult{Name: ch.Name, Cmd: strings.Join(ch.Argv, " "), ExitCode: -1}
	refuse := func(why string) (pipeline.CheckResult, error) {
		res.Output = "lockfile generation refused: " + why
		return res, nil
	}
	if !c.AllowLockGen || !ch.Network {
		return refuse("this profile does not enable lockfile generation")
	}
	if !c.Unsandboxed && c.Sandbox == nil {
		return res, ErrNoSandbox
	}
	plan, err := prepareLockGeneration(c.Dir, ch.Argv)
	if err != nil {
		return refuse(err.Error())
	}
	if err := plan.clearInstalledTree(); err != nil {
		return refuse("cannot clear node_modules before generation: " + err.Error())
	}
	// The lockfile is the runner's to create. One written by the agent is never
	// trusted, so it is removed and regenerated from package.json.
	if err := os.Remove(plan.lockPath); err != nil && !os.IsNotExist(err) {
		return refuse("cannot remove the existing package-lock.json: " + err.Error())
	}
	stamp := c.Before
	if stamp.IsZero() {
		stamp = time.Now()
	}
	before := stamp.UTC().Format("2006-01-02T15:04:05.000Z")

	var log strings.Builder
	fmt.Fprintf(&log, "generating the lockfile from package.json with --before=%s (registry %s, scripts disabled)\n", before, AllowedNPMRegistry)
	var first string
	for pass := 1; pass <= 2; pass++ {
		out, runErr, setupErr := c.execCmd(ctx, ch.Argv, true, before, to)
		if setupErr != nil {
			return res, setupErr
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if runErr != nil {
			res.Output = tailBytes([]byte(log.String()+stripANSI(out)+"\nnpm failed: "+runErr.Error()), 8000)
			return res, nil
		}
		sha, entries, verr := plan.verify(c.Dir)
		if verr != nil {
			res.Output = tailBytes([]byte(log.String()+"lockfile rejected after pass "+fmt.Sprint(pass)+": "+verr.Error()), 8000)
			return res, nil
		}
		if pass == 1 {
			first = sha
			fmt.Fprintf(&log, "pass 1: package-lock.json sha256=%s, %d entries, all resolved from %s with integrity\n", sha, entries, AllowedNPMRegistry)
			continue
		}
		if sha != first {
			res.Output = tailBytes([]byte(log.String()+"lockfile is not stable: a second generation changed it (sha256 "+first+" -> "+sha+")"), 8000)
			return res, nil
		}
		fmt.Fprintf(&log, "pass 2: a second generation left the lockfile byte-identical (sha256=%s)\n", sha)
	}
	res.Passed, res.ExitCode = true, 0
	res.Output = log.String()
	return res, nil
}

// validateLockGenProfile enforces the rules for the one profile capability that
// writes a file the agent cannot: the profile must own exactly package.json and
// package-lock.json for the directory, generate exactly once, and then install
// with the strict `npm ci` check afterwards.
func validateLockGenProfile(p Profile) error {
	genAt, genDir, gens := -1, "", 0
	for i, ch := range p.Checks {
		if dir, ok := LockGenNPM(ch.Argv); ok {
			genAt, genDir = i, dir
			gens++
		}
	}
	if !p.AllowLockfileGeneration {
		if gens > 0 {
			return fmt.Errorf("config: profile %q generates a lockfile without allow_lockfile_generation", p.Name)
		}
		return nil
	}
	if gens != 1 {
		return fmt.Errorf("config: profile %q enables allow_lockfile_generation and needs exactly one lockfile-generation check, found %d", p.Name, gens)
	}
	own := map[string]bool{}
	for _, f := range p.AllowedPaths {
		own[strings.TrimSpace(f)] = true
	}
	for _, f := range []string{path.Join(genDir, "package.json"), path.Join(genDir, "package-lock.json")} {
		if !own[f] {
			return fmt.Errorf("config: profile %q generates a lockfile but allowed_paths does not list %q; the profile must own the files it writes", p.Name, f)
		}
	}
	for _, f := range p.RequireFiles {
		if path.Clean(f) == path.Join(genDir, "package-lock.json") {
			return fmt.Errorf("config: profile %q lists %q in require_files, but the runner generates it after that check runs", p.Name, f)
		}
	}
	for _, ch := range p.Checks[genAt+1:] {
		if dir, ok := LockedNPMInstall(ch.Argv); ok && dir == genDir {
			return nil
		}
	}
	return fmt.Errorf("config: profile %q generates a lockfile but has no `npm ci --ignore-scripts` check for %q after it", p.Name, genDir)
}
