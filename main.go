package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/MalenkiySolovey/solovey-ui/app"
	"github.com/MalenkiySolovey/solovey-ui/cmd"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func runApp() {
	app := app.NewApp()
	service.SetInProcessRestart(app.RestartApp)

	err := app.Init()
	if err != nil {
		log.Fatal(err)
	}

	err = app.Start()
	if err != nil {
		log.Fatal(err)
	}

	sigCh := make(chan os.Signal, 1)
	// Trap shutdown signals
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM)
	for {
		sig := <-sigCh

		switch sig {
		case syscall.SIGHUP:
			app.RestartApp()
		default:
			app.Stop()
			return
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		runApp()
		return
	} else {
		cmd.ParseCmd()
	}
}
