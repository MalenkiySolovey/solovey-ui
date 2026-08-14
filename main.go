package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/MalenkiySolovey/solovey-ui/app"
	"github.com/MalenkiySolovey/solovey-ui/cmd"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func runApp() int {
	application := app.NewApp()
	service.SetInProcessRestart(application.RestartApp)

	err := application.Init()
	if err != nil {
		log.Print(err)
		return 1
	}

	err = application.Start()
	if err != nil {
		log.Print(err)
		application.Stop()
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	// Trap shutdown signals
	signal.Notify(sigCh, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	for {
		sig := <-sigCh

		switch sig {
		case syscall.SIGHUP:
			application.RestartApp()
		default:
			application.Stop()
			return 0
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		os.Exit(runApp())
	}
	os.Exit(cmd.ParseCmd())
}
