# Disklens

A native MyGo disk usage explorer for Windows and macOS. The upper pane is a compact, virtualized directory tree with folder icons and colored proportion bars; the lower pane is a label-free, size-proportional treemap. File extensions share vivid colors, with WinDirStat-style cushion shading to reveal nested groups. Both panes share a selection. The divider is resizable.

## Use

Build from source using the instructions below, then open `build/windows-amd64/Disklens.exe` on Windows or `build/darwin-universal/Disklens.app` on macOS. Generated binaries are not included in the repository.

- Enter a directory path or choose it with the `…` button.
- **Refresh** scans it and saves a new baseline without showing differences.
- **Refresh+** compares it with the previous saved baseline, shows signed folder differences, then saves the new scan as the next baseline. Increases are red; decreases are green. Removed directories remain visible for that comparison.
- **Depth** controls how many directory levels below the root are saved, initially 5. The root is level 0. Changing depth applies to the next saved snapshot. A comparison uses the depth of the previous snapshot; deeper folders show `—` until a baseline covers them. File rows show `—` because snapshots retain directory totals.
- Click a rectangle to select its file in the tree. Scroll up over the treemap to select its parent; scroll down to return to the previous child. Selection changes highlight the enclosing rectangle without zooming the whole map, unless the selection is above the current zoom root.
- Double-click a directory row to expand or collapse its children. Double-click a treemap rectangle to zoom into its directory. Use `↑` and `Root` to zoom out. Hovered paths and sizes appear beneath the map, not inside rectangles.
- **Largest files** shows the 100 largest files. Tree rows are ordered by descending allocated size. Arrow keys navigate and expand the tree.
- `Ctrl+R` / `Cmd+R` refreshes. Add Shift to compare. Cancel leaves the previous scan and snapshot intact.
- While scanning, a compact bottom meter shows estimated traversal progress and elapsed time. Folder and file details are available on hover. The estimate divides each discovered branch among its children; it is not a time or byte estimate and can pause on large branches. Preparation and saving use an indeterminate meter because their remaining work is unknown.

The entire directory is scanned, regardless of snapshot depth. Only the latest complete baseline per root is retained, locally, across restarts. Nothing is uploaded. Snapshot and settings files live under `%APPDATA%\Disklens` on Windows and `~/Library/Application Support/Disklens` on macOS. `DISKLENS_DATA` overrides that location for testing.

Sizes are allocated file bytes: Windows allocation information and macOS `stat` blocks. Hard-linked files are counted once, under the first encountered path. Symlinks, Windows reparse points (including junctions and cloud placeholders), and non-regular entries are skipped. Filesystem metadata, directory allocation, and shared APFS clone extents are not separately accounted for. Results are not a volume free-space measurement.

Unreadable entries produce a partial scan with a visible error count; hover the bottom status to inspect the first error. A partial scan neither compares nor replaces a complete baseline. Files can change during a scan; the scan is not a filesystem-level point-in-time snapshot.

## Develop

Requires Go 1.27.3 or later. MyGo 0.7.3 is pinned in `go.mod`. No Bun, webview, or C toolchain is needed.

```sh
go run .
go run . /path/to/scan
```

On Windows, `./run.ps1` locates the installed Go toolchain, builds, and opens the application. A folder is optional: `./run.ps1 -Folder "$env:USERPROFILE\Downloads"`.

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
$env:DISKLENS_EXE = (Resolve-Path build/Disklens-e2e.exe).Path
go test ./tests -v -count=1
go vet ./...
```

Coverage includes allocation totals, depth-limited snapshots with deeper aggregation, new/deleted/grown/shrunk directories, expansion, largest files, treemap selection and wheel navigation, depth edits, missing directories, restart persistence, and native rendering. Set `DISKLENS_CAPTURE` to an absolute PNG path to keep the test's screenshot.

On Windows, the scanner reads allocation sizes, timestamps and file IDs in 64 KiB directory batches through `GetFileInformationByHandleEx`. Ordinary files require no individual handles. Sparse and compressed files receive a separate allocation query. Filesystems that reject the batched information class at the root use the portable walker. The app uses directory enumeration, not WinDirStat's optional direct NTFS MFT reader, and does not request elevation or raw-volume access.

The scanner retains one full tree in memory, with virtualized rows and treemap geometry cached until zoom or dimensions change. The 100 largest files are selected on the scan worker with a bounded heap; comparison indexing is limited to directories covered by the saved snapshot. Memory still grows with the scanned entry count; scans of millions of files can require substantial memory. There is no background watcher or automatic rescanning. Full-drive memory limits and partial-scan comparisons are not implemented.

Built with [MyGo](https://mygo.egoist.dev/docs), with interaction references from [WinDirStat](https://github.com/windirstat/windirstat) and [disktree](https://github.com/tobi/disktree).
