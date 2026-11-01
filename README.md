# Disklens

<img src="assets/icon.png" alt="Disklens folder and magnifying glass icon" width="96" height="96">

App icon assets: [Windows ICO](assets/icon.ico) · [PNG](assets/icon.png) · [editable SVG](assets/icon.svg). These files are tracked in the repository for reuse in other builds. Windows resource builds use the ICO; MyGo macOS packaging uses the PNG.

A native MyGo disk usage explorer for Windows and macOS. The upper pane is a compact, virtualized directory tree with folder icons and colored proportion bars; the lower pane is a label-free, size-proportional treemap. File extensions share vivid colors, with WinDirStat-style cushion shading to reveal nested groups. Both panes share a selection. The divider is resizable.

## Use

On Windows, run `Disklens-0.1.6-Setup.exe` to install for your account, add Start menu shortcuts for the app and CLI terminal, and register an uninstaller in Windows Settings. The desktop shortcut is optional. Installation and removal preserve saved scans and baselines. The installed CLI is under `%LOCALAPPDATA%\Programs\Disklens\cli`; the installer adds it to your user PATH, so `disklens diff` works in new terminals. Uninstall removes that PATH entry.

Build from source using the instructions below, then open `build/windows-amd64/Disklens.exe` on Windows or `build/darwin-universal/Disklens.app` on macOS. Generated binaries are not included in the repository.

- Enter a directory path or choose it with the `â€¦` button.
- Right-click a directory row for **Properties** (Windows Properties / macOS Finder Get Info) or **Delete permanently**. A native warning confirms the exact path and defaults to Cancel. Deletion bypasses Recycle Bin/Trash, reuses the scanned tree, and runs four native deletion workers before removing directories bottom-up. The bottom meter counts processed items; **Cancel deletion** stops new work but cannot undo completed deletions. Only the affected folder and known hard-link peers are refreshed, with ancestor totals and the saved comparison updated; the baseline stays fixed. Other folders retain their last scanned state. The scan root and removed rows cannot be deleted; directory handles confine removal to the selected folder. Junctions are removed without deleting their targets. New or skipped entries trigger one cleanup pass inside the selected folder; locked or inaccessible entries remain visible with an incomplete-deletion status.
- **Refresh** saves the latest successful scan and compares it against your fixed baseline, if one exists. It never changes the baseline.
- **Set baseline** accepts the latest saved scan as the reference for future comparisons and clears the diff. First scan, inspect the result, then set the baseline. After cleanup, Refresh to see the space recovered; optionally set a new baseline to accept the current state. The tooltip identifies the saved scan time and root; this action does not rescan. Increases are red; decreases are green. The Change column uses a consistent logarithmic gradient by absolute bytes changed: small changes are muted, and changes of 10 GiB or more reach full intensity. Removed directories remain visible until the baseline is reset.
- **Depth** controls how many directory levels below the root are saved, initially 5. The root is level 0. Changing depth applies to the next saved snapshot. A comparison uses the shallower of the baseline and latest scan depths; deeper folders show `â€”` until a baseline covers them. File rows show `â€”` because snapshots retain directory totals.
- Click a rectangle to select its file in the tree. Scroll up over the treemap to select its parent; scroll down to return to the previous child. Selection changes highlight the enclosing rectangle without zooming the whole map, unless the selection is above the current zoom root.
- Double-click a directory row to expand or collapse its children. Double-click a treemap rectangle to zoom into its directory. Scroll up to select parent directories and zoom out beyond the current view. Hovered paths and sizes appear beneath the map, not inside rectangles.
- Tree rows are ordered by descending allocated size. Arrow keys navigate and expand the tree.
- `Ctrl+R` / `Cmd+R` refreshes. Cancel leaves the previous saved scan and baseline intact.
- While scanning, a compact bottom meter shows estimated traversal progress and elapsed time. Folder and file details are available on hover. The estimate divides each discovered branch among its children; it is not a time or byte estimate and can pause on large branches. Preparation and saving use an indeterminate meter because their remaining work is unknown.

The entire directory is scanned, regardless of snapshot depth. The interactive tree retains all scanned files and directories in RAM; snapshot depth only limits the saved directory records. Nodes store names and parent links, reconstructing full paths on demand. Modification timestamps use a compact representation, and child-list storage is allocated only for directories with children. After publishing a completed scan, the app releases unused scan memory back to the operating system. One baseline and one latest successful scan per root are retained locally across restarts, in one replaced state file. There is no accumulating history. Existing snapshots from earlier builds are treated as saved scans without a baseline; Refresh and explicitly Set baseline to begin comparisons. Nothing is uploaded. Snapshot and settings files live under `%USERPROFILE%\.disklens` on Windows and `~/Library/Application Support/Disklens` on macOS. Windows uses a shared profile directory so terminals launched by packaged applications cannot read a private AppData snapshot copy. On first launch from the Start menu, the desktop copies existing scans and settings from `%APPDATA%\Disklens`, preserving their baseline and timestamps and leaving the originals as backups. The CLI never imports an older private copy. `DISKLENS_DATA` overrides the location for testing.

Sizes are allocated file bytes: Windows allocation information and macOS `stat` blocks. Hard-linked files are counted once, under the first encountered path. Symlinks, Windows reparse points (including junctions and cloud placeholders), and non-regular entries are skipped. Filesystem metadata, directory allocation, and shared APFS clone extents are not separately accounted for. Results are not a volume free-space measurement.

Unreadable entries produce a partial scan with a visible error count; hover the bottom status to inspect the first error. A partial scan neither compares nor replaces the saved scan or baseline. Set baseline is disabled for partial scans. The CLI continues to report the previous successful saved comparison, with its timestamps. Files can change during a scan; the scan is not a filesystem-level point-in-time snapshot.

## Read-only CLI

```powershell
go build -tags cli -o build/cli/disklens.exe .
$env:PATH = "$(Resolve-Path build/cli);$env:PATH"
disklens diff
disklens diff "$env:USERPROFILE"
disklens diff --json
```

On macOS, build with `go build -tags cli -o build/cli/disklens .` and add that directory to PATH. The CLI build does not include MyGo or open a window. `run.ps1` also builds the Windows CLI. Use the console binary in `build/cli` for terminal commands, rather than the Windows GUI executable.

`disklens diff [path] [--json]` reads the latest saved comparison for exactly that root. The default is `C:\` on Windows and `/` on macOS. It never scans, resets a baseline, creates directories, or updates files. It works while the UI is closed and even if the scanned directory is no longer accessible. Manage scanning and baseline selection in the desktop app. This is a report of the last successful scan, not the current filesystem; inspect its timestamps.

While the desktop scans the same root, the CLI immediately returns the previous saved diff with `Scan in progress - showing the last completed scan.` JSON includes `scan_in_progress: true` (false when idle). If no scan has completed yet, it exits 1 with `SCAN_IN_PROGRESS`; if a completed scan exists without a baseline, it returns `NO_BASELINE` with the active flag. Status is observed when the command reads it and may change afterwards. It only covers scans started by this version of the desktop, using the same data directory.

A zero-byte `.scan.lock` file beside each root's state tracks activity through an OS-held lock. Finishing, canceling, or closing/crashing the desktop releases the lock automatically; an old file alone never means a scan is active. CLI checks open it read-only and never wait for the scan.

Deletion and its targeted snapshot update also hold this activity lock. A canceled or incomplete deletion saves reconciled sizes when they can be read completely. If reconciliation is incomplete, the previous snapshot remains unchanged and the UI asks for a refresh.

The default is aligned plain text with signed size changes, full directory paths, and baseline/scan timestamps with UTC offsets. Unchanged directories are omitted; changes are largest-first. An empty comparison says `No size changes.` There are no terminal color codes, so redirected output stays readable. A successful result (including no changes) exits 0.

```text
C:\ - saved comparison
Baseline:    2026-10-30 14:00:00 -07:00
Latest scan: 2026-10-30 16:30:00 -07:00
Depth: 5 | Allocated disk space

      Change  Directory
   +2.00 GiB  C:\Downloads
 -640.00 MiB  C:\Cache

+ increased usage; - freed space. Sizes include descendants; do not sum overlapping rows.
```

Add `--json` for one JSON object with exact integer byte counts:

```json
{
  "scan_in_progress": false,
  "status": "ok",
  "root": "C:\\",
  "baseline_at": "2026-10-30T20:00:00Z",
  "scanned_at": "2026-10-30T23:00:00Z",
  "depth": 5,
  "size_basis": "allocated_bytes",
  "includes_descendants": true,
  "changes": [
    {"delta": "+2.00 MiB", "delta_bytes": 2097152, "kind": "modified", "path": "C:\\Downloads"}
  ]
}
```

Each changed directory occupies one JSON line: a human-readable signed `delta`, exact `delta_bytes`, change `kind`, and full `path`. Positive means increased space usage; negative means space freed. Only directories with nonzero size changes appear, including added and removed directories. Metadata-only changes and empty-directory additions/removals are omitted. Entries are sorted by absolute delta descending, then path ascending. `depth` is the common comparison depth, with the root at zero. Directory totals include descendants: **do not sum overlapping entries**. The root entry, when present, is the net change. No changes, including immediately after Set baseline, produces `"changes": []`.

Errors exit 1 with a readable message on stderr. With `--json`, errors instead return a JSON object on stdout: `status: "error"`, a `code`, `message`, and `root` when applicable. Codes are `NO_SCAN`, `NO_BASELINE`, `STATE_READ_FAILED`, `STATUS_READ_FAILED`, `SCAN_IN_PROGRESS`, `INVALID_PATH`, and `INVALID_ARGUMENTS`. A baseline for a subdirectory does not substitute for a baseline of the entire drive. `disklens --help` prints usage; add `--json` for structured help.

## Develop

Requires Go 1.27.3 or later. MyGo 0.7.3 is pinned in `go.mod`. No Bun, webview, or C toolchain is needed.

```sh
go run .
go run . /path/to/scan
```

On Windows, `./run.ps1` locates the installed Go toolchain, builds the desktop app and CLI, and opens the application. A folder is optional: `./run.ps1 -Folder "$env:USERPROFILE\Downloads"`.

With NSIS `makensis` and MinGW `windres` on PATH, run `./build-installer.ps1` to build the Windows desktop and CLI and package `build/Disklens-<version>-Setup.exe`. The version comes from `mygo.json`. `build-resources.ps1` embeds `assets/icon.ico` into Windows executables; `mygo.json` supplies `assets/icon.png` for macOS packaging. The editable icon source is `assets/icon.svg`.

Build a portable Windows app on Windows:

```powershell
go build -ldflags '-H=windowsgui' -o build/windows-amd64/Disklens.exe .
```

Build a universal Mac app from Windows or macOS:

```sh
go run github.com/egoist/mygo/cmd/mygo@v0.7.3 build -debug -skip-dmg -platform darwin/universal
```

The Mac build produces an unsigned bundle for Apple Silicon and Intel. Cross-compilation has been verified. It has not been run on a Mac. Distribution signing and notarization require a Mac and a signing identity. These build commands retain MyGo's development inspector.

## Windows end-to-end verification

The maintained test launches the actual native app, drives its UI through MyGo's test-only automation socket, and verifies real filesystem scans and persisted snapshots. Automation is excluded from ordinary builds.

```powershell
go build -tags e2e -o build/Disklens-e2e.exe .
go build -tags cli -o build/cli/disklens.exe .
$env:DISKLENS_CLI_EXE = (Resolve-Path build/cli/disklens.exe).Path
$env:DISKLENS_EXE = (Resolve-Path build/Disklens-e2e.exe).Path
go test ./tests -v -count=1
go test -tags e2e . -run TestScanStatusCLIIntegration -v -count=1
go vet ./...
```

Coverage includes allocation totals, depth-limited snapshots with deeper aggregation, new/deleted/grown/shrunk directories, expansion, treemap selection and wheel navigation, depth edits, missing directories, restart persistence, fixed baselines across refreshes, explicit baseline reset, read-only CLI JSON, first-run errors, and native rendering. Set `DISKLENS_CAPTURE` to an absolute PNG path to keep the test's screenshot.

On Windows, the normal scanner uses four workers and `NtQueryDirectoryFile` (`FileIdFullDirectoryInformation`), with 4 MiB local buffers and 64 KiB remote buffers. File sizes, timestamps and identities come from directory records. Only zero-allocation compressed/sparse or unexpectedly large files need a separate allocation query. Unsupported filesystems use the portable walker.

The scanner retains one full tree in memory, with virtualized rows and treemap geometry cached until zoom or dimensions change. Comparison indexing is limited to directories covered by the saved snapshot. Memory still grows with the scanned entry count; scans of millions of files can require substantial memory. There is no background watcher or automatic rescanning. Full-drive memory limits and partial-scan comparisons are not implemented.

Built with [MyGo](https://mygo.egoist.dev/docs), with interaction references from [WinDirStat](https://github.com/windirstat/windirstat) and [disktree](https://github.com/tobi/disktree).

## Windows NTFS scanning

On Windows, launching the desktop requests administrator access automatically. Accept the Windows prompt to open the app; canceling closes it. NTFS fast scanning is the default, with no separate mode button. Choose the folder and refresh. The fast backend reads NTFS metadata directly in 4 MiB batches and reconstructs only the selected subtree. It uses Velocidex's Go NTFS parser for fragmented MFT extents. No MFT dump or scan cache is written to disk. Non-NTFS volumes and unavailable fast reads fall back to directory enumeration; the bottom bar identifies the backend and exposes a fallback reason on hover.

Fast-mode allocation uses the default data stream: ordinary nonresident allocation, compressed/sparse physical allocation, and eight-byte-rounded resident data, following WinDirStat's MFT accounting. Alternate data streams and directory allocation are excluded. Hard links are counted once in deterministic alphabetical traversal order; reparse points are skipped. A live filesystem can change during either scan. Reading raw NTFS metadata bypasses per-file access checks, so this mode requires administrator access.

An already-elevated launch does not prompt again. Snapshot-only CLI commands do not require elevation. macOS continues to use its filesystem metadata scanner. Test builds tagged `e2e` bypass the startup prompt so automation can run unattended; scanner fallback behavior is unchanged.

## Headless scan measurements

These tests run the scanner without creating the desktop or writing snapshots:

```powershell
$env:DISKLENS_SCAN_ROOT = "$env:USERPROFILE"
go test -tags e2e . -run '^TestScannerFilesystem$' -v -count=1
```

Set `DISKLENS_SCAN_BACKEND=mft` in an elevated shell to measure the NTFS backend. `DISKLENS_SCAN_RESULT` optionally writes a small JSON result with elapsed time, counts, allocated bytes and a tree checksum. Tests have a three-minute scan deadline. `DISKLENS_PARITY_ROOT` enables `TestScannerBackendParity`, comparing both scanners and checking allocation differences against Windows APIs; use a quiet fixture directory for strict equality and set `DISKLENS_REQUIRE_PARITY=1` to enforce it.

`TestScannerAllocationAndCancellation` verifies real ordinary, compressed, sparse and hard-linked files. Set `DISKLENS_NTFS_IMAGE` to the pinned go-ntfs module's `parser/test_data/test.ntfs.dd` fixture to run `TestNTFSImageFilesystem` and `TestNTFSUnbufferedImage`, which check reconstruction, allocation, subtree selection, cancellation, and aligned Windows disk reads.

`go test -tags e2e . -run '^Test(Deletion|Folder)' -v` checks deletion against disposable filesystem fixtures: native confirmation, cancellation, junction isolation, read-only and locked files, late-created entries, hard-link accounting, and targeted snapshot updates. Set `DISKLENS_DELETE_BENCH=1` to include a comparison of serial recursive removal and retained-tree parallel deletion on two generated 8,192-file fixtures; it never accepts a user directory as a deletion benchmark target.
