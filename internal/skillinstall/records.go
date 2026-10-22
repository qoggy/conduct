package skillinstall

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

func invalidRecords(reason string) error {
	return fmt.Errorf("invalid skill installation records: %s", reason)
}
func readRecords(path string, e Environment) (Records, error) {
	records := Records{SchemaVersion: 1, Installations: []Installation{}}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return records, nil
	}
	if err != nil {
		return records, fmt.Errorf("inspect skill records: %w", err)
	}
	if !info.Mode().IsRegular() {
		return records, invalidRecords("metadata must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return records, fmt.Errorf("read skill records: %w", err)
	}
	records = Records{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&records); err != nil {
		return records, fmt.Errorf("decode skill records: %w", err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return records, invalidRecords("unexpected trailing JSON")
	}
	if records.SchemaVersion != 1 || records.Installations == nil {
		return records, invalidRecords("unsupported schema or missing installations")
	}
	for recordIndex, record := range records.Installations {
		if record.Scope != Project && record.Scope != Global || record.Method != Copy && record.Method != Symlink || !nonempty(record.ConductVersion) || len(record.Targets) == 0 {
			return records, invalidRecords("invalid installation fields")
		}
		if record.Scope == Project {
			if !filepath.IsAbs(record.ProjectPath) || filepath.Clean(record.ProjectPath) != record.ProjectPath {
				return records, invalidRecords("projectPath must be an absolute physical path")
			}
		} else if record.ProjectPath != "" {
			return records, invalidRecords("global installation has projectPath")
		}
		if err = supportedAgents(record.Agents, e); err != nil {
			return records, invalidRecords(err.Error())
		}
		targetSeen := map[string]bool{}
		directories := 0
		links := 0
		for _, target := range record.Targets {
			if !validStoredPath(target.Path) || targetSeen[target.Path] {
				return records, invalidRecords("invalid or duplicate target path")
			}
			targetSeen[target.Path] = true
			switch target.Kind {
			case "directory":
				directories++
				if target.Target != "" {
					return records, invalidRecords("directory cannot have link target")
				}
			case "symlink":
				links++
				if !validStoredPath(target.Target) {
					return records, invalidRecords("invalid symlink target")
				}
			default:
				return records, invalidRecords("invalid target kind")
			}
		}
		if record.Method == Copy && links != 0 || record.Method == Symlink && (links == 0 || directories != 1) {
			return records, invalidRecords("method and targets disagree")
		}
		// Verify record shape without consulting mutable filesystem aliases.
		groups := map[string]bool{}
		for _, agent := range registry(e) {
			for _, id := range record.Agents {
				if id == agent.ID {
					groups[agent.project] = true
				}
			}
		}
		maximumTargets := len(groups)
		if record.Method == Symlink && !groups[".agents/skills"] {
			maximumTargets++
		}
		if len(record.Targets) > maximumTargets {
			return records, invalidRecords("too many targets for selected agents")
		}
		for index, target := range record.Targets {
			if target.Kind == "symlink" {
				found := false
				for _, central := range record.Targets {
					if central.Path == target.Target && central.Kind == "directory" {
						found = true
					}
				}
				if !found {
					return records, invalidRecords("symlink must reference the registered central directory")
				}
			}
			for _, other := range record.Targets[index+1:] {
				if overlaps(target.Path, other.Path) {
					return records, invalidRecords("installation targets overlap")
				}
			}
		}
		for _, previous := range records.Installations[:recordIndex] {
			if sameInstallationTargets(previous, record) {
				return records, invalidRecords("duplicate installation targets")
			}
		}
	}
	return records, nil
}
func validStoredPath(path string) bool {
	return nonempty(path) && !filepath.IsAbs(path) && filepath.Clean(path) == path && filepath.Base(path) == "conduct"
}
func writeRecords(path string, records Records) (returned error) {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode skill records: %w", err)
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".skill-installations-*.tmp")
	if err != nil {
		return fmt.Errorf("stage skill records: %w", err)
	}
	pending := file.Name()
	defer func() {
		if pending != "" {
			if err := os.Remove(pending); err != nil && !os.IsNotExist(err) {
				returned = errors.Join(returned, fmt.Errorf("remove staged skill records: %w", err))
			}
		}
	}()
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeError := file.Close()
	if err != nil {
		return fmt.Errorf("write skill records: %w", errors.Join(err, closeError))
	}
	if closeError != nil {
		return fmt.Errorf("close skill records: %w", closeError)
	}
	if err = os.Rename(pending, path); err != nil {
		return fmt.Errorf("replace skill records: %w", err)
	}
	pending = ""
	return nil
}
func lockRecords(path string) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create skill metadata directory: %w", err)
	}
	lockPath := filepath.Join(filepath.Dir(path), "skill-installations.lock")
	descriptor, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, fmt.Errorf("open skill record lock: %w", err)
	}
	file := os.NewFile(uintptr(descriptor), lockPath)
	deadline := time.Now().Add(30 * time.Second)
	for {
		err = unix.Flock(descriptor, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			closeError := file.Close()
			return nil, errors.Join(fmt.Errorf("lock skill records: %w", err), closeError)
		}
		if time.Now().After(deadline) {
			closeError := file.Close()
			return nil, errors.Join(fmt.Errorf("skill records are busy: %s", lockPath), closeError)
		}
		time.Sleep(25 * time.Millisecond)
	}
	return func() error {
		unlockError := unix.Flock(descriptor, unix.LOCK_UN)
		return errors.Join(unlockError, file.Close())
	}, nil
}

// validateRecordOverlaps reserves all registered physical destinations, even when
// a record cannot currently be synchronized. Unrelated runtime path failures do
// not block other records; their stored paths still protect them from removal.
func validateRecordOverlaps(records Records, e Environment) error {
	type reservedTarget struct{ path, kind, target, owner string }
	reserved := []reservedTarget{}
	add := func(target reservedTarget) error {
		for _, other := range reserved {
			if target.path == other.path {
				if target.kind != other.kind || target.target != other.target {
					return fmt.Errorf("conflicting registered skill target %s (%s and %s)", target.path, target.owner, other.owner)
				}
				continue
			}
			if overlaps(target.path, other.path) {
				return fmt.Errorf("registered skill targets overlap: %s (%s) and %s (%s)", target.path, target.owner, other.path, other.owner)
			}
		}
		reserved = append(reserved, target)
		return nil
	}
	for _, record := range records.Installations {
		root := record.ProjectPath
		if record.Scope == Global {
			root = e.Home
		}
		owner := fmt.Sprintf("scope=%s projectPath=%q", record.Scope, record.ProjectPath)
		for _, target := range record.Targets {
			path := filepath.Join(root, target.Path)
			link := ""
			if target.Target != "" {
				link = filepath.Join(root, target.Target)
			}
			if err := add(reservedTarget{path, target.Kind, link, owner}); err != nil {
				return err
			}
			physical, err := physicalParent(path)
			if err != nil {
				// An unusable alias is a per-record synchronization error. Its stored
				// physical path remains reserved above, without authorizing writes.
				continue
			}
			if physical != path {
				if err := add(reservedTarget{physical, target.Kind, link, owner}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
