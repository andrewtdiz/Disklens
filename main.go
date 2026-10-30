package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type scanResult struct {
	scan     *Scan
	baseline *Snapshot
	err      error
	notice   string
	saved    bool
}

type app struct {
	win                 *mygo.Window
	settings            Settings
	depth               float64
	split               float32
	scan                *Scan
	focus               *Node
	selected            *Node
	hover               *Node
	outline             ui.OutlineState[*Node]
	row                 int
	files               ui.ListState
	fileRow             int
	largest             []*Node
	tab                 int
	busy                bool
	cancel              context.CancelFunc
	results             chan scanResult
	progress            chan ScanProgress
	scanProgress        ScanProgress
	scanStarted         time.Time
	status              string
	baseline            string
	tiles               []tile
	mapRoot             *Node
	mapWidth, mapHeight float32
	selectionTrail      []*Node
	wheelRemainder      float32
	mapBitmap           *ui.Bitmap
}

func newApp() *app {
	home, _ := os.UserHomeDir()
	a := &app{settings: Settings{Path: home, Depth: 5}, split: 50, row: 0, fileRow: -1, status: "Choose a folder, then Refresh to scan."}
	if b, err := os.ReadFile(filepath.Join(dataDirectory(), "settings.json")); err == nil {
		_ = json.Unmarshal(b, &a.settings)
	}
	if a.settings.Depth < 1 || a.settings.Depth > 50 {
		a.settings.Depth = 5
	}
	a.depth = float64(a.settings.Depth)
	a.outline.List.Selected = &a.row
	a.files.Selected = &a.fileRow
	return a
}

func (a *app) start(diff bool) {
	if a.busy {
		return
	}
	path, err := filepath.Abs(filepath.Clean(a.settings.Path))
	if err != nil {
		a.status = err.Error()
		return
	}
	a.settings.Path = path
	a.settings.Depth = int(a.depth)
	settingsErr := saveJSON(filepath.Join(dataDirectory(), "settings.json"), a.settings)
	a.busy = true
	a.scanStarted = time.Now()
	a.scanProgress = ScanProgress{Phase: "Starting scan", Path: path}
	a.status = "Scanning…"
	a.results, a.progress = make(chan scanResult, 1), make(chan ScanProgress, 1)
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	results, progress, depth := a.results, a.progress, a.settings.Depth
	go func() {
		publish := func(p ScanProgress) {
			select {
			case <-progress:
			default:
			}
			select {
			case progress <- p:
			default:
			}
		}
		old, loadErr := loadSnapshot(path)
		s, err := walkDirectory(ctx, path, publish)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			publish(ScanProgress{Phase: "Preparing results", Path: path, Files: s.Files, Folders: s.Folders, Bytes: s.Bytes, Errors: s.Errors, Skipped: s.Skipped})
			s.Largest, err = largestFiles(ctx, s.Root)
		}
		r := scanResult{scan: s, err: err}
		if err == nil {
			if s.Errors > 0 {
				r.notice = fmt.Sprintf("Partial scan · %d unreadable entries · snapshot unchanged", s.Errors)
			} else if loadErr != nil {
				r.notice = "Cannot read saved snapshot: " + loadErr.Error()
			} else {
				p := ScanProgress{Phase: "Preparing snapshot", Path: path, Files: s.Files, Folders: s.Folders, Bytes: s.Bytes}
				publish(p)
				next := takeSnapshot(s, depth)
				if diff && old != nil {
					p.Phase = "Comparing snapshots"
					publish(p)
					compare(s.Root, old)
					r.baseline = old
				}
				p.Phase = "Saving snapshot"
				publish(p)
				if err := saveJSON(snapshotPath(path), next); err != nil {
					r.notice = "Snapshot could not be saved: " + err.Error()
				} else {
					r.saved = true
					if diff && old == nil {
						r.notice = "First snapshot saved · Refresh+ again to compare"
					}
				}
			}
			if settingsErr != nil {
				r.notice += " · Settings could not be saved: " + settingsErr.Error()
			}
		}
		results <- r
	}()
}

func (a *app) poll(c *ui.Context) {
	if !a.busy {
		return
	}
	select {
	case n := <-a.progress:
		a.scanProgress = n
	default:
	}
	select {
	case r := <-a.results:
		a.busy = false
		a.cancel()
		if r.err != nil {
			if r.err == context.Canceled {
				a.status = "Scan canceled · previous scan and snapshot retained"
			} else {
				a.status = r.err.Error()
			}
			return
		}
		a.scan, a.focus, a.selected = r.scan, r.scan.Root, r.scan.Root
		a.hover, a.mapRoot = nil, nil
		a.selectionTrail = nil
		a.outline = ui.OutlineState[*Node]{}
		a.row, a.fileRow = 0, -1
		a.outline.List.Selected = &a.row
		a.outline.Open.Add(a.scan.Root)
		a.largest = a.scan.Largest
		a.baseline = "Snapshot not saved"
		if r.saved {
			a.baseline = "Snapshot saved · depth " + fmt.Sprint(a.settings.Depth)
		}
		if r.baseline != nil {
			a.baseline = "Compared with " + r.baseline.At.Format("Jan 2, 15:04:05") + fmt.Sprintf(" · depth %d", r.baseline.Depth)
		}
		if r.scan.Errors > 0 {
			a.baseline = "Partial scan · comparison unavailable"
		}
		a.status = fmt.Sprintf("%s on disk · %d files · %d folders · %d links / special entries skipped", formatSize(a.scan.Root.Size), a.scan.Files, a.scan.Folders, a.scan.Skipped)
		if r.notice != "" {
			a.status = r.notice
		}
	default:
		c.After(100 * time.Millisecond)
	}
}

func theme() *ui.Theme {
	t := ui.DarkTheme()
	t.Background = ui.Hex("#202020")
	t.Surface = ui.Hex("#292929")
	t.SurfaceHover = ui.Hex("#383838")
	t.SurfacePressed = ui.Hex("#4a4a4a")
	t.Border = ui.Hex("#383838")
	t.Accent, t.AccentHover, t.AccentPressed = ui.Hex("#4a4a4a"), ui.Hex("#555555"), ui.Hex("#383838")
	t.AccentText, t.Text = ui.Hex("#fafafa"), ui.Hex("#e4e4e7")
	t.Focus, t.Selection = ui.Hex("#a1a1aa"), ui.Hex("#4a4a4a")
	t.FontSize, t.Spacing, t.Radius = 13, 4, 6
	t.Inverse, t.InverseText = ui.Hex("#383838"), ui.Hex("#fafafa")
	return t
}

func (a *app) view(c *ui.Context) {
	c.SetTheme(theme())
	a.poll(c)
	if c.Shortcut(ui.Cmd, ui.KeyR) {
		a.start(false)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyR) {
		a.start(true)
	}
	ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
		a.toolbar(c)
		ui.SplitVerticalPercent(c, &a.split, func() { a.tree(c) }, func() { a.mapView(c) }).Grow(1)
		ui.Row(c).Height(32).Padding(0, 16).Gap(12).AlignItems(ui.Center).Background(ui.Hex("#252525")).Children(func() {
			text := a.status
			if a.busy {
				p := a.scanProgress
				value := p.Fraction
				label := "Scanning"
				if p.Phase != "Scanning" {
					value = -1
					label = p.Phase
				}
				ui.Text(c.Key("scan-progress"), label).FontSize(11).SingleLine()
				previousTheme := c.Theme()
				meterTheme := *previousTheme
				meterTheme.Accent = ui.Hex("#60a5fa")
				c.SetTheme(&meterTheme)
				ui.Progress(c.Key("scan-meter"), value).Grow(1).Height(8).Tooltip(fmt.Sprintf("%s\n%d files | %s found\nEstimated traversal progress; branches vary in size", p.Path, p.Files, formatSize(p.Bytes)))
				c.SetTheme(previousTheme)
				if value >= 0 {
					ui.Text(c.Key("scan-percent"), fmt.Sprintf("~%.0f%%", min(value*100, 99))).FontSize(11).Width(42).SingleLine()
				}
				ui.Text(c, time.Since(a.scanStarted).Truncate(time.Second).String()).FontSize(11).TextColor(c.Theme().TextMuted).Width(55).SingleLine()
				return
			}
			label := ui.Text(c, text).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine().Grow(1)
			if a.scan != nil && a.scan.Errors > 0 {
				label.Tooltip(a.scan.FirstError)
			} else {
				label.Tooltip(text)
			}
			ui.Text(c, "LOCAL ONLY").FontSize(10).TextColor(c.Theme().TextMuted)
		})
	})
}

func (a *app) toolbar(c *ui.Context) {
	ui.Row(c).Height(68).Padding(14, 18).Gap(10).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		ui.Text(c, "Disklens").Bold().FontSize(19).Width(110)
		if ui.TextInput(c.Key("path"), &a.settings.Path).Label("Folder path").Placeholder("Folder path").Grow(1).Disabled(a.busy).Submitted() {
			a.start(false)
		}
		if ui.Button(c.Key("browse"), "…").Label("Choose folder").Width(34).Disabled(a.busy).Clicked() {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "Choose a folder to scan", DefaultPath: a.settings.Path, Directory: true, ShowHiddenFiles: true, TreatPackagesAsDirectories: true})
			if err != nil {
				a.status = err.Error()
			} else if len(paths) > 0 {
				a.settings.Path = paths[0]
				a.status = "Folder selected · Refresh to scan, or Refresh+ to compare"
			}
		}
		ui.Text(c, "Depth").FontSize(12).TextColor(c.Theme().TextMuted)
		if ui.NumberInput(c.Key("depth"), &a.depth, 1, 50, 1).Label("Snapshot depth").Width(180).Disabled(a.busy).Tooltip("Directory levels saved, below the scan root. Applies to the next snapshot.").Changed() {
			a.settings.Depth = int(a.depth)
			if err := saveJSON(filepath.Join(dataDirectory(), "settings.json"), a.settings); err != nil {
				a.status = err.Error()
			}
		}
		if a.busy {
			if ui.Button(c.Key("cancel"), "Cancel scan").Width(200).Clicked() {
				a.cancel()
			}
		} else {
			if ui.Button(c.Key("refresh"), "↻  Refresh").Width(99).Tooltip("Scan and save a new baseline · Ctrl/Cmd+R").Clicked() {
				a.start(false)
			}
			if ui.Button(c.Key("refresh-plus"), "↻  Refresh+").Width(104).Background(ui.Hex("#fafafa")).TextColor(ui.Hex("#292929")).Tooltip("Compare with the last saved scan, then save this scan · Ctrl/Cmd+Shift+R").Clicked() {
				a.start(true)
			}
		}
	})
}

func (a *app) tree(c *ui.Context) {
	previousTheme := c.Theme()
	compact := *previousTheme
	compact.FontSize, compact.Spacing, compact.Radius = 12, 2.5, 0
	compact.Surface = ui.Hex("#191919")
	c.SetTheme(&compact)
	defer c.SetTheme(previousTheme)
	ui.Column(c).Fill().FontSize(12).Children(func() {
		ui.Row(c).Height(28).Padding(2, 6).AlignItems(ui.Center).Gap(4).Children(func() {
			all := ui.Button(c.Key("all-files"), "All files").FontSize(12)
			if a.tab == 0 {
				all.Background(ui.Hex("#383838"))
			}
			if all.Clicked() {
				a.tab = 0
			}
			large := ui.Button(c.Key("largest-files"), "Largest files").FontSize(12)
			if a.tab == 1 {
				large.Background(ui.Hex("#383838"))
			}
			if large.Clicked() {
				a.tab = 1
			}
			ui.Box(c).Grow(1)
			ui.Text(c, a.baseline).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine()
		})
		if a.scan == nil {
			ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
				ui.Text(c, "See where your space goes.").FontSize(22)
				ui.Text(c, "Choose a directory above and press Refresh.").TextColor(c.Theme().TextMuted)
				ui.Text(c, "Refresh+ highlights changes since your last saved scan.").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			return
		}
		cols := []ui.TableColumn{{Title: "Name", MinWidth: 240}, {Title: "Size Proportion", Width: 120}, {Title: "Physical Size", Width: 125, Align: ui.End}, {Title: "Change", Width: 110, Align: ui.End}, {Title: "Last Change", Width: 160}}
		a.outline.Indent = 16
		if a.tab == 0 {
			before := a.row
			e := ui.OutlineTable(c.Key("file-tree"), &a.outline, cols, []*Node{a.scan.Root}, func(n *Node) []*Node { return n.Children }, func(n *Node, col int) { a.cell(c, n, col) }).Grow(1).Label("Directory explorer")
			if a.row != before {
				a.selected = a.outline.Item(a.row)
				a.selectionTrail = nil
			}
			if e.Submitted() {
				n := a.outline.Item(a.row)
				if n != nil && n.Dir {
					if a.outline.Open.Has(n) {
						a.outline.Open.Remove(n)
					} else {
						a.outline.Open.Add(n)
					}
					a.selected = n
				}
			}
		} else {
			before := a.fileRow
			e := ui.Table(c.Key("largest-table"), &a.files, cols, len(a.largest), func(row, col int) { a.cell(c, a.largest[row], col) }).Grow(1).Label("100 largest files")
			if a.fileRow != before && a.fileRow >= 0 && a.fileRow < len(a.largest) {
				a.selected = a.largest[a.fileRow]
			}
			if e.Submitted() && a.selected != nil {
				a.focus = a.selected.Parent
			}
		}
	})
}

func (a *app) cell(c *ui.Context, n *Node, col int) {
	switch col {
	case 0:
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(15, 14).Draw(func(p *ui.Painter, r ui.Rect) {
				if n.Dir {
					p.Fill(ui.Rect{X: r.X, Y: r.Y + 1, W: 7, H: 4}, ui.Hex("#e9b54a"), 1)
					p.Fill(ui.Rect{X: r.X, Y: r.Y + 4, W: 15, H: 9}, ui.Hex("#dba331"), 1)
					p.Fill(ui.Rect{X: r.X + 1, Y: r.Y + 4, W: 13, H: 7}, ui.Hex("#ffda70"), 0)
				} else {
					p.Fill(ui.Rect{X: r.X + 2, Y: r.Y + 1, W: 10, H: 12}, ui.Hex("#d8dce0"), 0)
					p.Fill(ui.Rect{X: r.X + 3, Y: r.Y + 8, W: 8, H: 4}, nodeColor(n), 0)
				}
			})
			label := n.Name
			if n.Parent == nil {
				label = n.Path
			}
			if n.Removed {
				label += "  (removed)"
			}
			ui.Text(c, label).SingleLine().Tooltip(n.Path)
		})
	case 1:
		fraction := float32(0)
		if a.scan.Root.Size > 0 {
			fraction = float32(float64(n.Size) / float64(a.scan.Root.Size))
		}
		depth := 0
		for parent := n.Parent; parent != nil; parent = parent.Parent {
			depth++
		}
		colors := []ui.Color{ui.Hex("#6663a5"), ui.Hex("#aa6868"), ui.Hex("#67a766"), ui.Hex("#a1a35d")}
		color := colors[min(depth, len(colors)-1)]
		ui.Box(c).Width(108).Height(14).Background(ui.Hex("#3b3b3b")).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Stroke(r, ui.Hex("#666666"), 0, 1)
			r.X++
			r.Y++
			r.W = (r.W - 2) * fraction
			r.H -= 2
			if r.W > 0 {
				p.Fill(r, color, 0)
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, color.Mix(ui.Hex("#ffffff"), 0.2), 0)
			}
		})
	case 2:
		ui.Text(c, formatSize(n.Size)).SingleLine()
	case 3:
		color := c.Theme().TextMuted
		if n.Compared && n.Delta > 0 {
			color = ui.Hex("#f87171")
		} else if n.Compared && n.Delta < 0 {
			color = ui.Hex("#4ade80")
		}
		ui.Text(c, formatDelta(n)).TextColor(color).SingleLine()
	case 4:
		ui.Text(c, n.Modified.Format("1/2/2006  3:04 PM")).SingleLine()
	}
}

func (a *app) selectNode(n *Node) {
	a.selected = n
	a.selectionTrail = nil
	for p := n.Parent; p != nil; p = p.Parent {
		a.outline.Open.Add(p)
	}
	a.tab = 0
	var find func(*Node) bool
	row := 0
	find = func(p *Node) bool {
		if p == n {
			a.row = row
			a.outline.List.ScrollTo(row, ui.Center)
			return true
		}
		row++
		if a.outline.Open.Has(p) {
			for _, ch := range p.Children {
				if find(ch) {
					return true
				}
			}
		}
		return false
	}
	find(a.scan.Root)
}

func main() {
	a := newApp()
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "Disklens", Width: 1280, Height: 860, MinWidth: 940, MinHeight: 580, Content: ui.View(a.view)})
		if len(os.Args) > 1 {
			a.settings.Path = os.Args[1]
			a.start(false)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
