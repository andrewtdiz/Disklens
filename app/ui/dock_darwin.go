//go:build !cli

package ui

import "github.com/ebitengine/purego/objc"

type dockSize struct {
	Width, Height float64
}

type dockFrame struct {
	X, Y float64
	Size dockSize
}

func configureDockIcon() {
	send := func(object objc.ID, selector string, args ...any) objc.ID {
		return object.Send(objc.RegisterName(selector), args...)
	}
	stringValue := func(value string) objc.ID {
		return send(objc.ID(objc.GetClass("NSString")), "stringWithUTF8String:", value)
	}
	bundle := send(objc.ID(objc.GetClass("NSBundle")), "mainBundle")
	path := send(bundle, "pathForResource:ofType:", stringValue("AppIcon"), stringValue("icns"))
	if path == 0 {
		return
	}
	icon := send(objc.ID(objc.GetClass("NSImage")), "alloc")
	icon = send(icon, "initWithContentsOfFile:", path)
	if icon == 0 {
		return
	}
	defer send(icon, "release")
	app := send(objc.ID(objc.GetClass("NSApplication")), "sharedApplication")
	tile := send(app, "dockTile")
	size := objc.Send[dockSize](tile, objc.RegisterName("size"))
	view := send(objc.ID(objc.GetClass("NSImageView")), "alloc")
	view = send(view, "initWithFrame:", dockFrame{Size: size})
	defer send(view, "release")
	send(view, "setImage:", icon)
	send(view, "setImageScaling:", uintptr(3))
	send(tile, "setContentView:", view)
	send(tile, "display")
}
