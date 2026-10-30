package main

import (
	"hash/fnv"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

type tile struct {
	node   *Node
	rect   ui.Rect
	branch bool
}

var palette = []ui.Color{ui.Hex("#4040ff"), ui.Hex("#ff4040"), ui.Hex("#40ef40"), ui.Hex("#efef40"), ui.Hex("#40eeee"), ui.Hex("#ef40ef"), ui.Hex("#efa850"), ui.Hex("#4095ff"), ui.Hex("#ff4095"), ui.Hex("#95ef40"), ui.Hex("#a040ff"), ui.Hex("#40ef95"), ui.Hex("#ff40b0"), ui.Hex("#40b0ef"), ui.Hex("#ef8540"), ui.Hex("#40efb0"), ui.Hex("#8540ff"), ui.Hex("#c0c0c0")}

func nodeColor(n *Node) ui.Color {
	if n.Removed {
		return ui.Hex("#4ade80")
	}
	if n.Dir {
		return ui.Hex("#a1a1aa")
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(filepath.Ext(n.Name))))
	return palette[int(h.Sum32())%len(palette)]
}

func layoutMap(root *Node, w, h float32) []tile {
	var out []tile
	var contents func(*Node, ui.Rect)
	var pack func([]*Node, int64, ui.Rect)
	contents = func(n *Node, r ui.Rect) {
		if r.W < 1 || r.H < 1 || n.Size == 0 {
			return
		}
		if len(n.Children) == 0 || r.W*r.H < 180 || r.W < 10 || r.H < 10 {
			out = append(out, tile{n, r, false})
			return
		}
		out = append(out, tile{n, r, true})
		positive := n.Children
		end := len(positive)
		for end > 0 && positive[end-1].Size == 0 {
			end--
		}
		pack(positive[:end], n.Size, r)
	}
	pack = func(nodes []*Node, total int64, r ui.Rect) {
		if len(nodes) == 0 || total == 0 || r.W < 1 || r.H < 1 {
			return
		}
		if len(nodes) == 1 {
			contents(nodes[0], r)
			return
		}
		var sum int64
		cut := 0
		for cut < len(nodes)-1 {
			next := sum + nodes[cut].Size
			if cut > 0 && next > total/2 && total/2-sum < next-total/2 {
				break
			}
			sum = next
			cut++
			if sum >= total/2 {
				break
			}
		}
		if cut == 0 {
			cut = 1
			sum = nodes[0].Size
		}
		fraction := float32(float64(sum) / float64(total))
		a, b := r, r
		if r.W >= r.H {
			a.W = r.W * fraction
			b.X += a.W
			b.W -= a.W
		} else {
			a.H = r.H * fraction
			b.Y += a.H
			b.H -= a.H
		}
		pack(nodes[:cut], sum, a)
		pack(nodes[cut:], total-sum, b)
	}
	contents(root, ui.Rect{W: w, H: h})
	return out
}

func contains(r ui.Rect, x, y float32) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (a *app) mapView(c *ui.Context) {
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(42).Padding(4, 14).Gap(10).AlignItems(ui.Center).Children(func() {
			if ui.Button(c.Key("map-up"), "↑").Label("Treemap parent").Width(30).Disabled(a.focus == nil || a.focus.Parent == nil).Clicked() {
				a.focus = a.focus.Parent
			}
			path := "Treemap"
			if a.focus != nil {
				path = a.focus.Path
			}
			if ui.Button(c.Key("map-root"), "Root").Disabled(a.scan == nil || a.focus == a.scan.Root).Clicked() {
				a.focus = a.scan.Root
			}
			ui.Text(c, path).SingleLine().Grow(1).FontSize(12).Tooltip(path)
		})
		box := ui.Box(c.Key("treemap")).Grow(1).Clip().Background(ui.Hex("#252525")).Label("Disk usage treemap")
		box.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputScroll || ev.DY == 0 {
				return false
			}
			if ev.Precise {
				a.wheelRemainder -= ev.DY
				for a.wheelRemainder >= 40 {
					a.navigateSelection(true)
					a.wheelRemainder -= 40
				}
				for a.wheelRemainder <= -40 {
					a.navigateSelection(false)
					a.wheelRemainder += 40
				}
			} else {
				a.navigateSelection(ev.DY < 0)
			}
			return true
		})
		if a.focus == nil || a.focus.Size == 0 {
			box.Children(func() {
				ui.Column(c).Fill().Center().Children(func() { ui.Text(c, "No allocated file data to display").TextColor(c.Theme().TextMuted) })
			})
		} else {
			r := box.Bounds()
			if a.mapRoot != a.focus || a.mapWidth != r.W || a.mapHeight != r.H {
				a.tiles = layoutMap(a.focus, r.W, r.H)
				a.mapBitmap = shadeMap(a.tiles, r.W, r.H)
				a.mapRoot, a.mapWidth, a.mapHeight = a.focus, r.W, r.H
			}
			x, y, over := box.PointerPosition()
			a.hover = nil
			if over {
				for i := len(a.tiles) - 1; i >= 0; i-- {
					if contains(a.tiles[i].rect, x, y) {
						a.hover = a.tiles[i].node
						break
					}
				}
			}
			if box.Clicked() && a.hover != nil {
				a.selectNode(a.hover)
			}
			if box.DoubleClicked() && a.hover != nil {
				n := a.hover
				if !n.Dir {
					n = n.Parent
				}
				if n != nil {
					a.focus = n
				}
			}
			tiles, hover, selected := a.tiles, a.hover, a.selected
			bitmap := a.mapBitmap
			box.Draw(func(p *ui.Painter, bounds ui.Rect) {
				if bitmap != nil {
					p.Image(bitmap, bounds, ui.FillBox)
				}
				for _, t := range tiles {
					if t.node == selected || t.node == hover {
						r := t.rect
						r.X += bounds.X
						r.Y += bounds.Y
						p.Stroke(r, ui.Hex("#fafafa"), 0, 2)
					}
				}
			})
		}
		ui.Row(c).Height(29).Padding(0, 14).Gap(12).AlignItems(ui.Center).Children(func() {
			n := a.hover
			if n == nil {
				n = a.selected
			}
			if n != nil {
				ui.Text(c, n.Path).SingleLine().Grow(1).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Text(c, formatSize(n.Size)).FontSize(11)
			} else {
				ui.Text(c, "Area represents allocated size · colors group file extensions").FontSize(11).TextColor(c.Theme().TextMuted)
			}
		})
	})
}

func (a *app) navigateSelection(parent bool) {
	if a.selected == nil {
		return
	}
	if parent {
		if a.selected.Parent == nil {
			return
		}
		trail := append(a.selectionTrail, a.selected)
		a.selectNode(a.selected.Parent)
		a.selectionTrail = trail
		for n := a.selected; n != nil; n = n.Parent {
			if n == a.focus {
				return
			}
		}
		a.focus = a.selected
	} else if len(a.selectionTrail) > 0 {
		trail := a.selectionTrail
		n := trail[len(trail)-1]
		if n.Parent != a.selected {
			a.selectionTrail = nil
			return
		}
		a.selectNode(n)
		a.selectionTrail = trail[:len(trail)-1]
	}
}
