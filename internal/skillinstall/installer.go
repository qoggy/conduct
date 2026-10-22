package skillinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

func CreatePlan(request Request, e Environment) (Plan, error) {
	metadata, err := metadataPath(e)
	if err != nil {
		return Plan{}, err
	}
	records, err := readRecords(metadata, e)
	if err != nil {
		return Plan{}, err
	}
	if err := validateRecordOverlaps(records, e); err != nil {
		return Plan{}, err
	}
	plan, err := createPlan(request, e, metadata)
	if err != nil {
		return Plan{}, err
	}
	record, err := storedInstallation(plan, e)
	if err != nil {
		return Plan{}, err
	}
	if err = validateRecordOverlaps(mergeInstallationRecords(records, record), e); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// ResolveMethod determines whether the selected physical destinations need a
// method choice. It does not inspect metadata or the unselected central copy.
func ResolveMethod(request Request, e Environment) (Method, error) {
	if request.Method != "" && request.Method != Copy && request.Method != Symlink {
		return "", fmt.Errorf("invalid skill installation method: %s", request.Method)
	}
	paths, _, err := resolveAgentTargetPaths(request.Agents, request.Scope, e)
	if err != nil {
		return "", err
	}
	return installationMethod(request.Method, len(paths)), nil
}

func installationMethod(requested Method, targetCount int) Method {
	if targetCount == 1 {
		return Copy
	}
	if requested == "" {
		return Symlink
	}
	return requested
}

func resolveAgentTargetPaths(agents []string, scope Scope, e Environment) (map[string][]string, string, error) {
	if scope != Project && scope != Global {
		return nil, "", fmt.Errorf("invalid skill installation scope: %s", scope)
	}
	selected := map[string]bool{}
	unique := []string{}
	for _, id := range agents {
		if !selected[id] {
			selected[id] = true
			unique = append(unique, id)
		}
	}
	if err := supportedAgents(unique, e); err != nil {
		return nil, "", err
	}
	root, err := rootFor(e, scope)
	if err != nil {
		return nil, "", err
	}
	paths := map[string][]string{}
	for _, agent := range registry(e) {
		if !selected[agent.ID] {
			continue
		}
		path := filepath.Join(root, agent.project, "conduct")
		if scope == Global {
			path = filepath.Join(agent.global, "conduct")
		}
		path, err = physicalParent(path)
		if err != nil {
			return nil, "", err
		}
		paths[path] = append(paths[path], agent.ID)
	}
	return paths, root, nil
}

func createPlan(request Request, e Environment, metadata string) (Plan, error) {
	if request.Scope != Project && request.Scope != Global {
		return Plan{}, fmt.Errorf("invalid skill installation scope: %s", request.Scope)
	}
	if request.Method != "" && request.Method != Copy && request.Method != Symlink {
		return Plan{}, fmt.Errorf("invalid skill installation method: %s", request.Method)
	}
	if !nonempty(e.Version) {
		return Plan{}, fmt.Errorf("conduct version is missing")
	}
	agents := []string{}
	seen := map[string]bool{}
	for _, id := range request.Agents {
		if !seen[id] {
			agents = append(agents, id)
			seen[id] = true
		}
	}
	if err := supportedAgents(agents, e); err != nil {
		return Plan{}, err
	}
	sort.Strings(agents)
	paths, root, err := resolveAgentTargetPaths(agents, request.Scope, e)
	if err != nil {
		return Plan{}, err
	}
	method := installationMethod(request.Method, len(paths))
	// Like the upstream installer, copy mode only uses the selected agent paths.
	// Resolve the canonical directory only when symlinks need a central copy.
	central := ""
	if method == Symlink {
		central, err = physicalParent(filepath.Join(root, ".agents/skills/conduct"))
		if err != nil {
			return Plan{}, err
		}
		if _, ok := paths[central]; !ok {
			paths[central] = []string{}
		}
	}
	plan := Plan{Scope: request.Scope, Method: method, Agents: agents, ConductVersion: e.Version, RecordPath: metadata, Targets: []Target{}}
	if request.Scope == Project {
		plan.ProjectPath = root
	}
	for path, owners := range paths {
		kind := "directory"
		target := ""
		if method == Symlink && path != central {
			kind = "symlink"
			target = central
		}
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return Plan{}, fmt.Errorf("inspect installation target: %w", err)
		}
		sort.Strings(owners)
		plan.Targets = append(plan.Targets, Target{Path: path, Kind: kind, Target: target, Agents: owners, Exists: info != nil})
	}
	sort.Slice(plan.Targets, func(i, j int) bool {
		if plan.Targets[i].Path == central {
			return true
		}
		if plan.Targets[j].Path == central {
			return false
		}
		return plan.Targets[i].Path < plan.Targets[j].Path
	})
	if err = validatePlanPaths(plan, e, root); err != nil {
		return Plan{}, err
	}
	return plan, nil
}
func validatePlanPaths(plan Plan, e Environment, root string) error {
	source, err := sourceDirectory()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	executable, err = physicalPath(executable)
	if err != nil {
		return err
	}
	for index, target := range plan.Targets {
		if source != "" && overlaps(target.Path, source) {
			return fmt.Errorf("skill target overlaps bundled source checkout: %s", target.Path)
		}
		if contains(target.Path, executable) {
			return fmt.Errorf("skill target contains current executable: %s", target.Path)
		}
		if contains(target.Path, root) || contains(target.Path, e.Home) || contains(target.Path, e.Cwd) {
			return fmt.Errorf("skill target contains an installation root: %s", target.Path)
		}
		if filepath.Base(target.Path) != "conduct" || !filepath.IsAbs(target.Path) {
			return fmt.Errorf("unsafe skill installation target: %s", target.Path)
		}
		if overlaps(target.Path, filepath.Dir(plan.RecordPath)) {
			return fmt.Errorf("skill target overlaps installation metadata: %s", target.Path)
		}
		for _, other := range plan.Targets[index+1:] {
			if overlaps(target.Path, other.Path) {
				return fmt.Errorf("skill installation targets overlap: %s and %s", target.Path, other.Path)
			}
		}
	}
	return nil
}

// planFromRecord uses registered physical targets, never a newly resolved agent
// destination. Current mappings or an identifiable historical installation must
// prove that replacing each directory is safe.
func planFromRecord(record Installation, e Environment) (plan Plan, returned error) {
	defer func() {
		if returned != nil {
			returned = fmt.Errorf("skill installation scope=%s projectPath=%q: %w", record.Scope, record.ProjectPath, returned)
		}
	}()
	if record.Scope == Project {
		e.Cwd = record.ProjectPath
	}
	root, err := rootFor(e, record.Scope)
	if err != nil {
		return Plan{}, err
	}
	if record.Scope == Project && root != record.ProjectPath {
		return Plan{}, invalidRecords("projectPath is no longer physical")
	}
	metadata, err := metadataPath(e)
	if err != nil {
		return Plan{}, err
	}
	selected := map[string]bool{}
	for _, id := range record.Agents {
		selected[id] = true
	}
	permitted := map[string]bool{}
	var mappingError error
	permit := func(path string) {
		physical, err := physicalParent(path)
		if err != nil {
			// A changed logical alias cannot redirect a registered physical target.
			// Historical identity below is required when this mapping is unavailable.
			mappingError = errors.Join(mappingError, err)
			return
		}
		permitted[physical] = true
	}
	for _, agent := range registry(e) {
		if !selected[agent.ID] {
			continue
		}
		path := filepath.Join(root, agent.project, "conduct")
		if record.Scope == Global {
			path = filepath.Join(agent.global, "conduct")
		}
		permit(path)
	}
	if record.Method == Symlink {
		permit(filepath.Join(root, ".agents/skills/conduct"))
	}
	plan = Plan{Scope: record.Scope, ProjectPath: record.ProjectPath, Method: record.Method, Agents: record.Agents, ConductVersion: e.Version, RecordPath: metadata, Targets: []Target{}}
	for _, target := range record.Targets {
		path := filepath.Join(root, target.Path)
		physical, err := physicalParent(path)
		if err != nil {
			return Plan{}, err
		}
		if physical != path {
			return Plan{}, fmt.Errorf("recorded physical target parent changed: %s", path)
		}
		if !permitted[path] {
			safe, err := safeHistoricalSkillTarget(path)
			if err != nil {
				return Plan{}, err
			}
			if !safe {
				return Plan{}, errors.Join(fmt.Errorf("historical conduct skill target cannot be verified: %s", path), mappingError)
			}
		}
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return Plan{}, fmt.Errorf("inspect registered target: %w", err)
		}
		absolute := Target{Path: path, Kind: target.Kind, Exists: info != nil}
		if target.Target != "" {
			absolute.Target = filepath.Join(root, target.Target)
		}
		plan.Targets = append(plan.Targets, absolute)
	}
	// The central directory must be refreshed before links are checked/created.
	sort.SliceStable(plan.Targets, func(i, j int) bool { return plan.Targets[i].Kind == "directory" && plan.Targets[j].Kind != "directory" })
	if err := validatePlanPaths(plan, e, root); err != nil {
		return Plan{}, err
	}
	return plan, nil
}
func storedInstallation(plan Plan, e Environment) (Installation, error) {
	root := plan.ProjectPath
	var err error
	if plan.Scope == Global {
		root, err = rootFor(e, Global)
		if err != nil {
			return Installation{}, err
		}
	}
	record := Installation{Scope: plan.Scope, ProjectPath: plan.ProjectPath, ConductVersion: plan.ConductVersion, Agents: plan.Agents, Method: plan.Method, Targets: []StoredTarget{}}
	for _, target := range plan.Targets {
		path, err := filepath.Rel(root, target.Path)
		if err != nil {
			return Installation{}, err
		}
		stored := StoredTarget{Path: path, Kind: target.Kind}
		if target.Target != "" {
			stored.Target, err = filepath.Rel(root, target.Target)
			if err != nil {
				return Installation{}, err
			}
		}
		record.Targets = append(record.Targets, stored)
	}
	return record, nil
}

// InstallError reports all destinations that may have changed; no rollback is claimed.
type InstallError struct {
	Cause          error
	ChangedTargets []string
}

func (e *InstallError) Error() string {
	return fmt.Sprintf("skill installation failed; changed targets were not rolled back: %v: %v", e.ChangedTargets, e.Cause)
}
func (e *InstallError) Unwrap() error { return e.Cause }
func installFiles(plan Plan) (changed []string, returned error) {
	for _, target := range plan.Targets {
		canonical, err := physicalParent(target.Path)
		if err != nil {
			return changed, err
		}
		if canonical != target.Path {
			return changed, fmt.Errorf("skill target parent changed: %s", target.Path)
		}
		info, err := os.Lstat(target.Path)
		if err != nil && !os.IsNotExist(err) {
			return changed, fmt.Errorf("inspect skill target: %w", err)
		}
		if target.Kind == "symlink" && info != nil && info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(target.Path)
			if err != nil {
				return changed, fmt.Errorf("read skill link: %w", err)
			}
			linkPath := link
			if !filepath.IsAbs(linkPath) {
				linkPath = filepath.Join(filepath.Dir(target.Path), linkPath)
			}
			resolved, err := filepath.EvalSymlinks(linkPath)
			if err == nil && resolved == target.Target {
				continue
			}
			// Any resolution failure means this link is not a correct installed link.
			// Replace only the final link; never traverse its broken or cyclic target.
		}
		if err = os.MkdirAll(filepath.Dir(target.Path), 0755); err != nil {
			return changed, fmt.Errorf("create skill target parent: %w", err)
		}
		canonical, err = physicalParent(target.Path)
		if err != nil {
			return changed, err
		}
		if canonical != target.Path {
			return changed, fmt.Errorf("skill target parent changed: %s", target.Path)
		}
		changed = append(changed, target.Path)
		if err = os.RemoveAll(target.Path); err != nil {
			return changed, fmt.Errorf("remove old skill target %s: %w", target.Path, err)
		}
		if target.Kind == "directory" {
			err = copyBundle(target.Path)
		} else {
			var relative string
			relative, err = filepath.Rel(filepath.Dir(target.Path), target.Target)
			if err == nil {
				err = os.Symlink(relative, target.Path)
			}
		}
		if err != nil {
			return changed, fmt.Errorf("install skill target %s: %w", target.Path, err)
		}
	}
	return changed, nil
}
func Execute(plan Plan, e Environment) (result Result, returned error) {
	verified, err := CreatePlan(Request{Agents: plan.Agents, Scope: plan.Scope, Method: plan.Method}, e)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(plan, verified) {
		return result, fmt.Errorf("skill installation plan changed; review a new plan")
	}
	unlock, err := lockRecords(plan.RecordPath)
	if err != nil {
		return result, err
	}
	defer func() {
		if err := unlock(); err != nil {
			returned = errors.Join(returned, fmt.Errorf("release skill record lock: %w", err))
		}
	}()
	fresh, err := CreatePlan(Request{Agents: plan.Agents, Scope: plan.Scope, Method: plan.Method}, e)
	if err != nil {
		return result, err
	}
	if !reflect.DeepEqual(plan, fresh) {
		return result, fmt.Errorf("skill installation plan changed; review a new plan")
	}
	records, err := readRecords(plan.RecordPath, e)
	if err != nil {
		return result, err
	}
	record, err := storedInstallation(plan, e)
	if err != nil {
		return result, err
	}
	changed, err := installFiles(plan)
	if err != nil {
		return result, &InstallError{Cause: err, ChangedTargets: changed}
	}
	records = mergeInstallationRecords(records, record)
	if err = writeRecords(plan.RecordPath, records); err != nil {
		return result, &InstallError{Cause: err, ChangedTargets: changed}
	}
	return plan, nil
}
