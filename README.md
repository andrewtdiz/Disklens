# Disklens

<img src="app/assets/icon.png" alt="Disklens folder and magnifying glass icon" width="96" height="96">

A native disk usage explorer for Windows and macOS, built with [MyGo](https://mygo.egoist.dev/docs). Explore allocated disk space through a directory tree and treemap, then compare scans to see what grew or shrank.

## Getting started

On Windows, run the release's setup `.exe`. It installs the desktop app; add its `cli` directory to PATH to use the CLI. For macOS or source builds, see [Build](#build).

**Permissions:** Windows requests administrator access for fast NTFS scanning. On macOS, enable Disklens under **System Settings → Privacy & Security → Full Disk Access**, then reopen it to scan protected folders without repeated prompts.

1. Choose a folder and click **Refresh** (`Ctrl+R` / `Cmd+R`). The first saved scan becomes your baseline.
2. Refresh later to compare changes in the directory tree and treemap.
3. **Set baseline** replaces your reference explicitly. Hover over a change for scan coverage details.

| Control | Behavior |
| --- | --- |
| Directory tree | Double-click folders to expand; use arrow keys to navigate. |
| Treemap | Click to select, double-click to zoom, scroll up/down to select a parent/previous child. |
| Depth | Limits saved directory levels, initially 5; the entire folder is still scanned. |
| Right-click folder | Open Properties/Get Info or **Delete permanently**. |
| Cancel | Stop scanning while preserving the previous saved scan and baseline. |

**Permanent deletion bypasses Trash/Recycle Bin.** Confirm the exact folder before proceeding. Canceling cannot undo completed deletions. Affected sizes are updated; the baseline stays fixed.

## Scans and saved data

- Sizes represent allocated file bytes, not volume free space. Hard links count once; symlinks, Windows reparse points, and special entries are skipped. Shared APFS clone extents are not separately accounted for.
- Unreadable entries produce a saved partial scan that can be set as a baseline. Partial scans and baselines are labeled; unreadable data is excluded, affected comparisons are marked uncertain (older snapshots without coverage paths make the whole comparison uncertain). Files can change during a scan.
- Each root retains one latest successful scan and one baseline. Nothing is uploaded; there is no automatic rescanning. Large scans retain a full tree in memory.

| Platform | Settings and snapshots |
| --- | --- |
| Windows | `%USERPROFILE%\.disklens` |
| macOS | `~/Library/Application Support/Disklens` |

Use `DISKLENS_DATA` to override the data directory for testing.

## CLI

```sh
disklens scan /path/to/folder
disklens diff /path/to/folder --refresh --json
disklens diff /path/to/folder
disklens coverage /path/to/folder
disklens baseline /path/to/folder
disklens --help
```

CLI scans use the same native scanner and snapshots as the app. `scan` and `diff --refresh` save fresh results; plain `diff` only reads saved data. `baseline` adopts the latest saved scan. Use `--depth 1-50` when scanning to control retained history; the default is 5, or the previous scan’s depth. The default root is `C:\` on Windows and `/` on macOS. During a scan, it returns the previous saved comparison and reports scanning activity.

Output includes timestamps and signed size changes; JSON includes exact byte counts. CLI contributors are additive: direct-file rows exclude child directories, while subtree rows include everything below the saved depth limit. Desktop directory totals remain inclusive. Uncertain rows are separated from increases/decreases; all rows reconcile to observed net change. On Windows, launch the CLI elevated for fast NTFS access; otherwise it falls back to directory enumeration. On macOS, CLI access depends on the launching terminal’s permissions. Progress goes to stderr; partial scans succeed with explicit coverage metadata. Success exits 0; errors exit 1, with structured error codes when using `--json`.

## Build

Requires **Go 1.27.3+**. Run commands from the repository root. MyGo 0.7.3 is pinned in `go.mod`.

```sh
go run ./app
go run ./app /path/to/scan
go build -tags cli -o build/cli/disklens ./app
```

Add `build/cli` to PATH to use `disklens` from any directory. On Windows, name the CLI binary `disklens.exe`.

### macOS

```sh
go run github.com/egoist/mygo/cmd/mygo@v0.7.3 build -skip-dmg -platform darwin/arm64
mkdir -p ~/Applications
ditto build/darwin-arm64/Disklens.app ~/Applications/Disklens.app
open ~/Applications/Disklens.app
```

Use `darwin/universal` for Apple Silicon and Intel. Local builds are ad-hoc signed; distribution requires signing and notarization. Scanning uses native `getattrlistbulk` metadata reads with up to eight workers.

### Windows

```powershell
./run.ps1
./run.ps1 -Folder "$env:USERPROFILE\Downloads"
./build-installer.ps1
```

`run.ps1` builds and opens the app. Windows icon compilation requires MinGW `windres`; installer packaging also requires NSIS `makensis` on PATH. Fast NTFS scanning falls back to directory enumeration when unavailable.

## Updates

Packaged apps check for updates automatically. Use **Help → Check for Updates…**
for a manual check. See [release instructions](docs/releases.md) for publishing
signed updates and configuring macOS certificate signing.

## Repository

```text
app/
  main.go, cli_main.go, cli.go   Launchers and CLI
  ui/                          Windows, tree, treemap, dialogs
  utils/                       Scanning, deletion, snapshots, storage
  assets/                      Original PNG, ICO, SVG and Windows resources
tests/                         Desktop and CLI integration tests
```

UI and CLI share `utils`, which has no UI dependency. Platform implementations use `_darwin.go` and `_windows.go` suffixes. `mygo.json` defines packaging and the app icon.

## Verify

Build the test desktop and CLI, then run filesystem and integration checks:

```sh
go build -tags e2e -o build/Disklens-e2e ./app
go build -tags cli -o build/cli/disklens ./app
export DISKLENS_EXE="$PWD/build/Disklens-e2e"
export DISKLENS_CLI_EXE="$PWD/build/cli/disklens"
go test -tags e2e ./app/... -count=1
go test ./tests -count=1
go vet ./...
```

On Windows, add `.exe` to both output paths and set the variables with PowerShell's `$env:NAME = (Resolve-Path path).Path` syntax. Test automation is excluded from normal builds.
