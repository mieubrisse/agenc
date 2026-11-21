package server

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// The auto-commit limits. A pass over either one is held back rather than
// committed: nothing is staged, nothing is touched on disk, and the user is
// told (pauseReasonBulkChange).
//
// The guard exists because the sync loop commits whatever appears, at any
// volume, and history keeps it forever. On 2026-10-05 a practice drill wrote
// program output into the exobrain's writeable copy and the loop committed
// eight snapshots of it in thirteen minutes: 149.5 MB, 53% of that repo's
// entire history, which nothing reported for a month (KEV-223). An ignore rule
// for the directory was written six minutes into the incident and was already
// too late; a pattern list covers only what someone foresaw, so the signal here
// is volume.
//
// Calibrated on 2026-11-21 against every auto-sync commit in the three synced
// repos with history (22,368 commits). Ordinary work sits far below both
// limits: the 99.9th percentile pass is at most 17 files and 1.8 MB in any of
// them, and outside the incident and a handful of deliberate bulk imports the
// largest pass was 44 files and the heaviest 4.7 MB. Every one of the
// incident's six large commits, 261 to 9,561 files each, is over the file
// limit. The deliberate imports (2,023 journal files, 222 reference documents, a
// 219-file vendored skill) would be held too — correctly: each is exactly the
// kind of pass someone should look at before it becomes permanent, and
// committing it by hand releases it.
//
// Three known edges, accepted:
//   - The limits are per pass. Output written slowly enough to stay under them
//     on every tick is still committed, and so is the part of a fast write
//     that a sweep tick catches mid-stream under the limit; the next tick then
//     holds the rest. Nothing observed has had the slow shape.
//   - What is measured is not re-checked after `git add -A`, so files that land
//     in the milliseconds between the two are committed unmeasured.
//   - A path is counted whether or not its content is already in history, so
//     moving more than autoCommitMaxFiles tracked files is held too. The
//     calibration above counted moves the same way, and found none that large.
const (
	autoCommitMaxFiles = 200
	autoCommitMaxBytes = 20 * bytesPerMB
)

const bytesPerMB = 1000 * 1000

// maxDirsInBulkChangeDetail bounds the directory breakdown in the notification.
// The point is to show where the bulk landed, which is almost always one or two
// directories.
const maxDirsInBulkChangeDetail = 5

// changeVolume is what an auto-commit would add to history in one pass.
type changeVolume struct {
	files int
	bytes int64
	// largestDirs is where the files are, most files first, capped at
	// maxDirsInBulkChangeDetail.
	largestDirs []*dirVolume
}

// dirVolume is the part of a changeVolume that sits directly in one directory.
type dirVolume struct {
	// relDirpath is relative to the repo root; "." is the root itself.
	relDirpath string
	files      int
	bytes      int64
}

func summarizePendingChanges(changes []pendingChange) *changeVolume {
	volume := &changeVolume{}
	byDir := map[string]*dirVolume{}
	for _, change := range changes {
		volume.files++
		volume.bytes += change.bytes

		// git prints forward slashes on every platform.
		relDirpath := path.Dir(change.relPath)
		dir, found := byDir[relDirpath]
		if !found {
			dir = &dirVolume{relDirpath: relDirpath}
			byDir[relDirpath] = dir
		}
		dir.files++
		dir.bytes += change.bytes
	}

	dirs := make([]*dirVolume, 0, len(byDir))
	for _, dir := range byDir {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].files != dirs[j].files {
			return dirs[i].files > dirs[j].files
		}
		if dirs[i].bytes != dirs[j].bytes {
			return dirs[i].bytes > dirs[j].bytes
		}
		return dirs[i].relDirpath < dirs[j].relDirpath
	})
	if len(dirs) > maxDirsInBulkChangeDetail {
		dirs = dirs[:maxDirsInBulkChangeDetail]
	}
	volume.largestDirs = dirs
	return volume
}

func (v *changeVolume) exceedsAutoCommitLimits() bool {
	return v.files > autoCommitMaxFiles || v.bytes > autoCommitMaxBytes
}

func formatMB(bytes int64) string {
	return fmt.Sprintf("%.1f MB", float64(bytes)/bytesPerMB)
}

func titleForBulkChange(repoName string, volume *changeVolume) string {
	return fmt.Sprintf("Auto-commit held back on %s: %d new or changed files, %s in one pass", repoName, volume.files, formatMB(volume.bytes))
}

// buildBulkChangeDetail is the notification body for a held-back pass. It says
// what was found and where, because the directory is usually all a reader needs
// to recognise generated output, and it gives both ways out.
func buildBulkChangeDetail(repoDirpath string, volume *changeVolume) string {
	var sb strings.Builder
	sb.WriteString("# Auto-commit held back: too much new content in one pass\n\n")
	sb.WriteString(fmt.Sprintf("**Path:** %s\n\n", repoDirpath))
	sb.WriteString(fmt.Sprintf(
		"**Pending when this was posted:** %d new or changed files, %s — over the auto-commit limit of %d files or %s in one pass.\n\n",
		volume.files, formatMB(volume.bytes), autoCommitMaxFiles, formatMB(autoCommitMaxBytes),
	))
	sb.WriteString("Nothing has been staged, committed or deleted: every file is still on disk exactly as it was written. " +
		"The loop declined to put this into git history because a pass this size is almost always generated output — " +
		"program output, build artifacts, a dataset or model cache — and once pushed, history keeps it forever. " +
		"Ordinary editing comes nowhere near either limit.\n\n")

	sb.WriteString("## Where it is\n\n")
	sb.WriteString("| Directory | Files | Size |\n|---|---|---|\n")
	for _, dir := range volume.largestDirs {
		sb.WriteString(fmt.Sprintf("| `%s` | %d | %s |\n", dir.relDirpath, dir.files, formatMB(dir.bytes)))
	}
	sb.WriteString("\n")

	sb.WriteString("## How to resolve\n\n")
	sb.WriteString("- **If it is generated output**, keep it out of git: add its directory to `.gitignore`, or delete it.\n")
	sb.WriteString("- **If it belongs in the repo**, commit it yourself, which is the review this pass was waiting for:\n\n")
	sb.WriteString("```sh\n")
	sb.WriteString(fmt.Sprintf("git -C %s add -A\n", repoDirpath))
	sb.WriteString(fmt.Sprintf("git -C %s commit -m \"<what this is>\"\n", repoDirpath))
	sb.WriteString("```\n\n")
	sb.WriteString("Either way the next tick finds the working tree back within the limits and syncs as usual; the loop pushes your commit.\n\n")
	sb.WriteString(pauseResolutionNote(pauseReasonBulkChange))
	return sb.String()
}
