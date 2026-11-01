//go:build !cli

package ui

import "disklens/app/utils"

func (a *app) applyDeletion(r utils.DeletionResult) {
	utils.ApplyDeletion(a.scan, r, a.outline.Open.Remove)
	parent := r.Target.Parent
	a.focus, a.selected = parent, parent
	a.hover, a.mapRoot, a.mapColorRoot = nil, nil, nil
	a.selectionTrail = nil
	a.outline.Open.Add(parent)
	a.selectNode(parent)
}
