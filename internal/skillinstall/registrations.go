package skillinstall

import "sort"

// mergeInstallationRecords replaces registrations only for targets written by
// this installation. Unselected targets keep their original version and method.
func mergeInstallationRecords(records Records, installed Installation) Records {
	merged := Records{SchemaVersion: records.SchemaVersion, Installations: []Installation{}}
	written := map[string]bool{}
	for _, target := range installed.Targets {
		written[target.Path] = true
	}
	appendRecord := func(record Installation) {
		for index, previous := range merged.Installations {
			if sameInstallationTargets(previous, record) {
				// Pruning overlapping batches can leave the same central copy.
				// Keep one registration, including all of its original agents.
				selected := map[string]bool{}
				for _, agents := range [][]string{previous.Agents, record.Agents} {
					for _, id := range agents {
						selected[id] = true
					}
				}
				record.Agents = []string{}
				for id := range selected {
					record.Agents = append(record.Agents, id)
				}
				sort.Strings(record.Agents)
				merged.Installations[index] = record
				return
			}
		}
		merged.Installations = append(merged.Installations, record)
	}
	for _, record := range records.Installations {
		if record.Scope != installed.Scope || record.ProjectPath != installed.ProjectPath {
			appendRecord(record)
			continue
		}
		needed := map[string]bool{}
		for _, target := range record.Targets {
			if target.Kind == "symlink" && !written[target.Path] {
				needed[target.Target] = true
			}
		}
		retained := []StoredTarget{}
		links := 0
		for _, target := range record.Targets {
			if written[target.Path] && !needed[target.Path] {
				continue
			}
			retained = append(retained, target)
			if target.Kind == "symlink" {
				links++
			}
		}
		if len(retained) == 0 {
			continue
		}
		record.Targets = retained
		if links == 0 {
			record.Method = Copy
		}
		appendRecord(record)
	}
	appendRecord(installed)
	return merged
}

func sameInstallationTargets(left, right Installation) bool {
	if left.Scope != right.Scope || left.ProjectPath != right.ProjectPath || len(left.Targets) != len(right.Targets) {
		return false
	}
	targets := map[StoredTarget]bool{}
	for _, target := range left.Targets {
		targets[target] = true
	}
	for _, target := range right.Targets {
		if !targets[target] {
			return false
		}
	}
	return true
}
