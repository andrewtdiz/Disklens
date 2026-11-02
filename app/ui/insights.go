//go:build !cli

package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"disklens/app/utils"
	"github.com/egoist/mygo/ui"
)

type coverageRow struct {
	Snapshot string
	Path     string
	Reason   string
}

func (a *app) refreshInsights() {
	a.contributors = nil
	a.coverageRows = nil
	a.contributionFocus = ""
	a.contributionList = ui.ListState{}
	a.coverageList = ui.ListState{}
	if a.savedState == nil || a.savedState.Latest == nil {
		return
	}
	if a.savedState.Baseline != nil {
		a.report = utils.SnapshotDiff(a.savedState)
		a.contributors = a.report.Changes
	}
	for _, item := range []struct {
		name     string
		snapshot *utils.Snapshot
	}{{"Baseline", a.savedState.Baseline}, {"Latest", a.savedState.Latest}} {
		if item.snapshot == nil {
			continue
		}
		if item.snapshot.UnreadableEntries > len(item.snapshot.Issues) {
			a.coverageRows = append(a.coverageRows, coverageRow{item.name, item.snapshot.Root, "Older snapshot: unreadable paths were not saved. Rescan to collect coverage details."})
		}
		for _, issue := range item.snapshot.Issues {
			a.coverageRows = append(a.coverageRows, coverageRow{item.name, issue.Path, issue.Reason})
		}
	}
}

func (a *app) insights(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(6, 8).Gap(6).Children(func() {
			for _, tab := range []struct{ key, label string }{{"", "Growth"}, {"tree", "Explorer"}, {"coverage", "Coverage"}} {
				if ui.Button(c.Key("tab-"+tab.label), tab.label).Disabled(a.insightsTab == tab.key).Clicked() {
					a.insightsTab = tab.key
				}
			}
		})
		if a.savedState != nil && a.savedState.Latest != nil {
			latest := a.savedState.Latest
			age := time.Since(latest.At).Truncate(time.Minute)
			freshness := "Scanned just now"
			if age >= time.Minute {
				freshness = "Scanned " + age.String() + " ago"
			}
			coverage := "0 unreadable entries"
			if latest.UnreadableEntries > 0 {
				coverage = fmt.Sprintf("Partial · %d unreadable entries", latest.UnreadableEntries)
			}
			ui.Text(c.Key("scan-freshness"), freshness+" · "+coverage).FontSize(12).Padding(2, 8).Tooltip(latest.At.Format(time.RFC3339))
			c.After(time.Minute)
		}
		ui.Text(c, a.baseline).FontSize(11).Padding(2, 8).SingleLine().Tooltip(a.baseline)
		if a.insightsTab == "tree" || a.scan == nil {
			a.tree(c)
			return
		}
		if a.insightsTab == "coverage" {
			a.coverageView(c)
			return
		}
		a.growthView(c)
	})
}

func (a *app) growthView(c *ui.Context) {
	if a.savedState == nil || a.savedState.Baseline == nil {
		ui.Text(c, "Scan to create a baseline and start measuring growth.").Padding(12, 8)
		return
	}
	if a.report.BaselineOnly {
		ui.Text(c.Key("baseline-ready"), "Baseline ready — scan again to measure changes.").Padding(12, 8)
		ui.Text(c, "Use Explorer to inspect current sizes.").Padding(0, 8).TextColor(c.Theme().TextMuted)
		return
	}
	increased, decreased, uncertain, observed := a.report.IncreasedBytes, a.report.DecreasedBytes, a.report.UncertainBytes, a.report.NetBytes
	if a.contributionFocus != "" {
		increased, decreased, uncertain, observed = 0, 0, 0, 0
		for _, row := range a.contributors {
			observed += row.DeltaBytes
			if row.Uncertain {
				uncertain += row.DeltaBytes
			} else if row.DeltaBytes > 0 {
				increased += row.DeltaBytes
			} else {
				decreased -= row.DeltaBytes
			}
		}
	}
	ui.Text(c, fmt.Sprintf("Increases %s · Decreases %s", utils.FormatSize(increased), utils.FormatSize(decreased))).Padding(6, 8)
	net := "Observed net " + utils.SignedSize(observed)
	if a.report.Partial {
		net += " · coverage uncertainty " + utils.SignedSize(uncertain)
	}
	ui.Text(c, net).Padding(0, 8).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Text(c, "Rows do not overlap. Allocation decreases may not equal recovered disk space.").Padding(6, 8).FontSize(11).TextColor(c.Theme().TextMuted)
	if a.report.Partial {
		ui.Text(c, "Partial comparison — uncertain rows are excluded from increases/decreases. See Coverage.").Padding(2, 8).FontSize(11)
	}
	if a.contributionFocus != "" {
		ui.Row(c).Padding(4, 8).Gap(8).Children(func() {
			if ui.Button(c.Key("all-contributors"), "All contributors").Clicked() {
				a.contributionFocus = ""
				a.contributors = a.report.Changes
				a.contributionList = ui.ListState{}
				c.After(0)
			}
			if ui.Button(c.Key("parent-contributors"), "Parent").Disabled(a.contributionFocus == a.report.Root).Clicked() {
				relative, _ := filepath.Rel(a.report.Root, filepath.Dir(a.contributionFocus))
				a.showContributionGroup(relative)
				c.After(0)
			}
			ui.Text(c, a.contributionFocus).SingleLine().Tooltip(a.contributionFocus)
		})
	}
	if len(a.contributors) == 0 {
		ui.Text(c, "No measured size changes.").Padding(12, 8)
		return
	}
	columns := []ui.TableColumn{{Title: "Change", Width: 90}, {Title: "Contributor", MinWidth: 150}, {Title: "Scope", Width: 115}, {Title: "Actions", Width: 180}}
	var navigate func()
	ui.Table(c.Key("growth-contributors"), &a.contributionList, columns, len(a.contributors), func(row, col int) {
		change := a.contributors[row]
		switch col {
		case 0:
			label := change.Delta
			color := changeColor(change.DeltaBytes)
			if change.Uncertain {
				label += " ?"
				color = c.Theme().TextMuted
			}
			ui.Text(c, label).SingleLine().TextColor(color).Tooltip("? means incomplete coverage affects this change")
		case 1:
			relative, _ := filepath.Rel(a.report.Root, change.Path)
			if relative == "." {
				relative = a.report.Root
			}
			ui.Text(c, relative).SingleLine().Tooltip(change.Path)
		case 2:
			scope := "Direct files only"
			if change.Scope == "subtree" {
				scope = "Whole subtree"
			}
			ui.Text(c, scope).FontSize(11).SingleLine().Tooltip("Direct files exclude all saved child-directory totals. At the saved depth limit, a subtree includes its descendants.")
		case 3:
			ui.Row(c).Gap(4).Children(func() {
				if ui.Button(c.Key("explore-"+change.Path), "Explore").Width(66).Padding(0, 4).Disabled(a.busy).Clicked() {
					a.exploreContributor(change.Path)
				}
				if ui.Button(c.Key("group-"+change.Path), "Group").Width(58).Padding(0, 4).Disabled(a.busy).Tooltip("Replace this list with the containing folder's child totals and direct-file remainder").Clicked() {
					path := filepath.Dir(change.Path)
					rel, err := filepath.Rel(a.report.Root, path)
					if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
						rel = "."
					}
					navigate = func() { a.showContributionGroup(rel) }
				}
				if change.Scope == "subtree" {
					rel, _ := filepath.Rel(a.report.Root, change.Path)
					level := 0
					if rel != "." {
						level = strings.Count(rel, string(filepath.Separator)) + 1
					}
					if level < a.report.Depth && ui.Button(c.Key("expand-"+change.Path), "+").Width(26).Padding(0).Disabled(a.busy).Tooltip("Expand: replace this list with this folder's children and direct-file remainder").Clicked() {
						navigate = func() { a.showContributionGroup(rel) }
					}
				}
			})
		}
	}).Grow(1).Label("Non-overlapping growth contributors")
	if navigate != nil {
		navigate()
		c.After(0)
	}
}

func (a *app) coverageView(c *ui.Context) {
	ui.Text(c, "Unreadable entries are excluded. A failed folder can hide additional descendants.").Padding(8).FontSize(12)
	if len(a.coverageRows) == 0 {
		ui.Text(c, "No unreadable entries in the saved comparison.").Padding(8)
		return
	}
	columns := []ui.TableColumn{{Title: "Snapshot", Width: 90}, {Title: "Path", MinWidth: 200}, {Title: "Reason", MinWidth: 240}}
	ui.Table(c.Key("coverage-details"), &a.coverageList, columns, len(a.coverageRows), func(row, col int) {
		entry := a.coverageRows[row]
		value := entry.Snapshot
		if col == 1 {
			value = entry.Path
		}
		if col == 2 {
			value = entry.Reason
		}
		ui.Text(c, value).SingleLine().Tooltip(value)
	}).Grow(1).Label("Unreadable paths and reasons")
}

func (a *app) showContributionGroup(relative string) {
	a.contributionFocus = filepath.Join(a.report.Root, relative)
	a.contributors = utils.ContributionRows(a.savedState, relative, true)
	a.contributionList = ui.ListState{}
}

func (a *app) exploreContributor(path string) {
	relative, err := filepath.Rel(a.scan.Root.FullPath(), path)
	if err != nil {
		return
	}
	node := a.scan.Root
	if relative != "." {
		for _, part := range strings.Split(relative, string(filepath.Separator)) {
			var next *utils.Node
			for _, child := range node.Children() {
				if child.Dir && child.Name == part {
					next = child
					break
				}
			}
			if next == nil {
				return
			}
			node = next
		}
	}
	a.selectNode(node)
	a.focus = node
	a.insightsTab = "tree"
}
