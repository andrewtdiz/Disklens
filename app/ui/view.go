//go:build !cli

package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"disklens/app/utils"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

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
	a.memory.observe(a.scan != nil, a.busy || a.deleting)
	if c.Shortcut(ui.Cmd, ui.KeyR) {
		a.start()
	}
	ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
		a.toolbar(c)
		ui.SplitVerticalPercent(c, &a.split, func() { a.tree(c) }, func() { a.mapView(c) }).Grow(1)
		if a.busy || a.deleting {
			a.progressBar(c)
		}
	})
	if a.cleanupPending && !a.busy {
		a.cleanupPending = false
		done := a.cleanupDone
		a.cleanupDone = nil
		go func() {
			if done != nil {
				<-done
			}
			debug.FreeOSMemory()
			a.memory.requestTrim()
		}()
	}
	if a.busy || a.deleting || a.deleteConfirm {
		c.After(100 * time.Millisecond)
	}
}

func (a *app) progressBar(c *ui.Context) {
	p := a.scanProgress
	value, label := p.Fraction, "Scanning"
	if a.deleting {
		label = "Deleting"
	}
	if p.Phase != "Scanning" && p.Phase != "Reading NTFS" && p.Total == 0 {
		value = -1
		label = "Finishing"
		if p.Phase == "Setting baseline" {
			label = "Setting baseline"
		}
	}
	ui.Row(c.Key("scan-status")).Height(32).Padding(0, 16).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c.Key("scan-progress"), label).FontSize(11).SingleLine()
		previous := c.Theme()
		progressTheme := *previous
		progressTheme.Accent = ui.Hex("#60a5fa")
		c.SetTheme(&progressTheme)
		ui.Progress(c.Key("scan-meter"), value).Grow(1).Height(8).Tooltip(p.Path)
		c.SetTheme(previous)
		if value >= 0 {
			percent := fmt.Sprintf("~%.0f%%", min(value*100, 99))
			if p.Phase == "Reading NTFS" || p.Total > 0 {
				percent = fmt.Sprintf("%.0f%%", value*100)
			}
			ui.Text(c.Key("scan-percent"), percent).FontSize(11).Width(42).SingleLine()
		}
		ui.Text(c, time.Since(a.scanStarted).Truncate(time.Second).String()).FontSize(11).Width(55).SingleLine()
	})
}

func (a *app) toolbar(c *ui.Context) {
	previousTheme := c.Theme()
	compact := *previousTheme
	compact.FontSize, compact.Spacing, compact.Radius = 12, 2.5, 2
	c.SetTheme(&compact)
	defer c.SetTheme(previousTheme)
	ui.Row(c.Key("toolbar")).Height(38).Padding(5, 8).Gap(6).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(c.Theme().Border).Children(func() {
		if ui.TextInput(c.Key("path"), &a.settings.Path).Label("Folder path").Placeholder("Folder path").Height(26).Grow(1).Disabled(a.busy).Submitted() {
			a.start()
		}
		if toolbarButton(c.Key("browse"), "", "Choose folder", folderIcon, 28).Disabled(a.busy).Clicked() {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: a.win, Title: "Choose a folder to scan", DefaultPath: a.settings.Path, Directory: true, ShowHiddenFiles: true, TreatPackagesAsDirectories: true})
			if err != nil {
				a.status = err.Error()
			} else if len(paths) > 0 {
				a.settings.Path = paths[0]
				a.status = "Folder selected - Refresh to scan"
			}
		}
		ui.Text(c, "Depth").FontSize(12).TextColor(c.Theme().TextMuted)
		previousDepth := a.depth
		input := ui.TextInput(c.Key("depth"), &a.depthText).Label("Snapshot depth").Width(36).Height(26).Disabled(a.busy).Tooltip("Folder levels retained for comparisons.")
		if input.Changed() {
			if value, err := strconv.Atoi(a.depthText); err == nil && value >= 1 && value <= 50 {
				a.depth = float64(value)
			}
		}
		if toolbarButton(c.Key("depth-down"), "", "Decrease depth", minusIcon, 26).Disabled(a.busy || a.depth <= 1).Clicked() || input.Shortcut(0, ui.KeyDown) {
			a.depth = max(1, a.depth-1)
			a.depthText = strconv.Itoa(int(a.depth))
		}
		if toolbarButton(c.Key("depth-up"), "", "Increase depth", plusIcon, 26).Disabled(a.busy || a.depth >= 50).Clicked() || input.Shortcut(0, ui.KeyUp) {
			a.depth = min(50, a.depth+1)
			a.depthText = strconv.Itoa(int(a.depth))
		}
		if !input.Focused() {
			a.depthText = strconv.Itoa(int(a.depth))
		}
		if a.depth != previousDepth {
			a.settings.Depth = int(a.depth)
			if err := utils.SaveJSON(filepath.Join(utils.DataDirectory(), "settings.json"), a.settings); err != nil {
				a.status = err.Error()
			}
		}
		ui.Row(c.Key("scan-actions")).Width(190).Shrink(0).Height(26).Gap(6).AlignItems(ui.Center).Children(func() {
			if a.deleting {
				if ui.Button(c.Key("cancel-delete"), "Cancel deletion").Width(190).Height(26).Clicked() {
					a.cancel()
				}
			} else if a.busy {
				if ui.Button(c.Key("cancel"), "Cancel scan").Width(190).Height(26).Padding(0, 8).Tooltip(a.scanProgress.Phase).Clicked() {
					a.cancel()
				}
			} else {
				if toolbarButton(c.Key("refresh"), "Refresh", "Refresh", refreshIcon, 84).Tooltip(a.status).Clicked() {
					a.start()
				}
				canSet := a.savedState != nil && a.scan != nil
				path, err := filepath.Abs(filepath.Clean(a.settings.Path))
				canSet = canSet && err == nil && utils.SnapshotPath(path) == utils.SnapshotPath(a.scan.Root.FullPath())
				hint := "Scan this folder first."
				if canSet {
					hint = "Compare future scans against this scan."
					if a.savedState.Latest.UnreadableEntries > 0 {
						hint += fmt.Sprintf(" %d unreadable entries excluded.", a.savedState.Latest.UnreadableEntries)
					}
				}

				if ui.Button(c.Key("set-baseline"), "Set baseline").Height(26).Width(100).Padding(0, 6).Disabled(!canSet).Tooltip(hint).Clicked() {
					a.setBaseline()
				}
			}
		})
	})
}

func (a *app) tree(c *ui.Context) {
	c.DisableAnimations()
	previousTheme := c.Theme()
	compact := *previousTheme
	compact.FontSize, compact.Spacing, compact.Radius = 12, 2.5, 0
	compact.Surface = ui.Hex("#191919")
	c.SetTheme(&compact)
	defer c.SetTheme(previousTheme)
	ui.Column(c).Fill().FontSize(12).Children(func() {
		if a.scan == nil {
			ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
				ui.Text(c, "Choose a folder to scan.").TextColor(c.Theme().TextMuted)
			})
			return
		}
		cols := []ui.TableColumn{{Title: "Name", MinWidth: 240}, {Title: "Size Proportion", Width: 120}, {Title: "Physical Size", Width: 125, Align: ui.End}, {Title: "Change", Width: 110, Align: ui.End}, {Title: "Last Change", Width: 160}}
		a.outline.Indent = 16
		before := a.row
		headerTheme := compact
		headerTheme.TextMuted = compact.Text
		headerTheme.Font = ""
		c.SetTheme(&headerTheme)
		e := ui.OutlineTable(c.Key("file-tree"), &a.outline, cols, []*utils.Node{a.scan.Root}, func(n *utils.Node) []*utils.Node { return n.Children() }, func(n *utils.Node, col int) {
			c.SetTheme(&compact)
			a.folderMenu(c, n, col)
			c.SetTheme(&headerTheme)
		}).Grow(1).Label("Directory explorer")
		c.SetTheme(&compact)
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

	})
}

func (a *app) cell(c *ui.Context, n *utils.Node, col int) {
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
				label = n.FullPath()
			}
			if n.Removed && n.Uncertain {
				label += "  (unavailable)"
			} else if n.Removed {
				label += "  (removed)"
			}
			ui.Text(c, label).SingleLine().Tooltip(n.FullPath())
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
		ui.Text(c, utils.FormatSize(n.Size)).SingleLine()
	case 3:
		color := c.Theme().TextMuted
		if n.Compared && n.Delta != 0 {
			color = changeColor(n.Delta)
		}
		hint := "Includes subfolders."
		if n.Uncertain && n.Compared {
			hint = "Incomplete scan. This change may include unreadable files."
		}
		ui.Text(c, utils.FormatDelta(n)).TextColor(color).SingleLine().Tooltip(hint)
	case 4:
		ui.Text(c, n.Modified.Time().Format("1/2/2006  3:04 PM")).SingleLine()
	}
}

func changeColor(delta int64) ui.Color {
	strength := float32(min(1, math.Log1p(math.Abs(float64(delta))/(1<<20))/math.Log1p(10*1024)))
	if delta > 0 {
		return ui.Hex("#b49a9a").Mix(ui.Hex("#ff6666"), strength)
	}
	return ui.Hex("#96ad9d").Mix(ui.Hex("#4ade80"), strength)
}
