//go:build !cli

package ui

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func Run() {
	ready, err := prepareDesktop()
	if err != nil {
		log.Print(err)
	}
	if !ready {
		return
	}
	var folder string
	if len(os.Args) > 1 {
		folder = os.Args[1]
	}
	a := newApp()
	stopMemory := a.memory.start()
	defer stopMemory()
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{Title: "Disklens", BackgroundColor: "#202020", Hidden: true, Width: 1280, Height: 860, MinWidth: 940, MinHeight: 580, Content: ui.View(a.view)})
		a.win.SetOpacity(0)
		a.win.OnReadyToShow(func() { a.win.SetOpacity(1) })
		a.win.Show()
		if folder != "" {
			a.settings.Path = folder
			a.start()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
