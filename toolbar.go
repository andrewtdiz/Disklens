//go:build !cli

package main

import "github.com/egoist/mygo/ui"

var folderIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 4h5l2 2h5v7H2Z" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linejoin="round"/></svg>`))
var minusIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M4 8h8" fill="none" stroke="currentColor" stroke-width="1.2"/></svg>`))
var plusIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M4 8h8M8 4v8" fill="none" stroke="currentColor" stroke-width="1.2"/></svg>`))
var refreshIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M13 7a5 5 0 1 0-1 4M13 3v4H9" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round"/></svg>`))

func toolbarButton(c *ui.Context, text, label string, icon *ui.SVG, width float32) ui.Element {
	padding := float32(6)
	if text == "" {
		padding = 0
	}
	return ui.Button(c, "").Label(label).Tooltip(label).Width(width).Height(26).Padding(0, padding).Gap(5).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(16, 16).Shrink(0)
		if text != "" {
			ui.Text(c, text).FontSize(12).SingleLine()
		}
	})
}
