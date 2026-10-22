package skillinstall

import (
	"errors"
	"fmt"
	"os"
)

// Sync only follows explicit registrations; an empty scope selects all records.
// No-record and no-match calls remain read-only and do not create a lock.
func Sync(e Environment, scope Scope) (report SyncReport, returned error) {
	report.Items = []SyncItem{}
	if scope != "" && scope != Project && scope != Global {
		return report, fmt.Errorf("invalid skill synchronization scope: %s", scope)
	}
	metadata, err := metadataPath(e)
	if err != nil {
		return report, err
	}
	records, err := readRecords(metadata, e)
	if err != nil {
		return report, err
	}
	cwd := ""
	if scope == Project {
		cwd, err = rootFor(e, Project)
		if err != nil {
			return report, err
		}
	}
	matches := func(record Installation) bool {
		return scope == "" || scope == Global && record.Scope == Global || scope == Project && record.Scope == Project && record.ProjectPath == cwd
	}
	for _, record := range records.Installations {
		if matches(record) {
			report.Matched++
		}
	}
	if report.Matched == 0 {
		return report, nil
	}
	// Check all registrations for destructive overlap, including unselected scopes.
	if err = validateRecordOverlaps(records, e); err != nil {
		return report, err
	}
	unlock, err := lockRecords(metadata)
	if err != nil {
		return report, err
	}
	defer func() {
		if err := unlock(); err != nil {
			returned = errors.Join(returned, fmt.Errorf("release skill record lock: %w", err))
		}
	}()
	records, err = readRecords(metadata, e)
	if err != nil {
		return report, err
	}
	if err = validateRecordOverlaps(records, e); err != nil {
		return report, err
	}
	report.Matched = 0
	for index, record := range records.Installations {
		if !matches(record) {
			continue
		}
		report.Matched++
		item := SyncItem{Scope: record.Scope, ProjectPath: record.ProjectPath, ConductVersion: record.ConductVersion}
		recordFailure := func(err error) error {
			return fmt.Errorf("synchronize skill scope=%s projectPath=%q: %w", record.Scope, record.ProjectPath, err)
		}
		if record.Scope == Project {
			info, err := os.Stat(record.ProjectPath)
			if os.IsNotExist(err) {
				item.Status = "skipped"
				item.Error = "registered project does not exist"
				report.Skipped++
				report.Items = append(report.Items, item)
				continue
			}
			if err != nil || !info.IsDir() {
				item.Status = "failed"
				if err != nil {
					item.Error = err.Error()
				} else {
					item.Error = "registered project is not a directory"
				}
				report.Failed++
				report.Items = append(report.Items, item)
				returned = errors.Join(returned, recordFailure(errors.New(item.Error)))
				continue
			}
		}
		plan, err := planFromRecord(record, e)
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			report.Failed++
			report.Items = append(report.Items, item)
			returned = errors.Join(returned, err)
			continue
		}
		item.Targets = plan.Targets
		changed, err := installFiles(plan)
		if err != nil {
			failure := &InstallError{Cause: err, ChangedTargets: changed}
			item.Status = "failed"
			item.Error = failure.Error()
			report.Failed++
			report.Items = append(report.Items, item)
			returned = errors.Join(returned, recordFailure(failure))
			continue
		}
		updated, err := storedInstallation(plan, e)
		if err != nil {
			failure := &InstallError{Cause: err, ChangedTargets: changed}
			item.Status = "failed"
			item.Error = failure.Error()
			report.Failed++
			report.Items = append(report.Items, item)
			returned = errors.Join(returned, recordFailure(failure))
			continue
		}
		original := records.Installations[index]
		records.Installations[index] = updated
		if err = writeRecords(metadata, records); err != nil {
			records.Installations[index] = original
			failure := &InstallError{Cause: err, ChangedTargets: changed}
			item.Status = "failed"
			item.Error = failure.Error()
			report.Failed++
			report.Items = append(report.Items, item)
			returned = errors.Join(returned, recordFailure(failure))
			continue
		}
		item.Status = "synced"
		item.ConductVersion = e.Version
		report.Synced++
		report.Items = append(report.Items, item)
	}
	return report, returned
}
