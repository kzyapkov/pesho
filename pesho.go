// pesho, named after Peter the Saint
// is the gatekeeper of the premises of initlab.org
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/kzyapkov/pesho/config"
	"github.com/kzyapkov/pesho/door"
)

var configPath = flag.String("config", "/etc/pesho/config.json", "Configuration file")

type pesho struct {
	door      door.Door
	closeOnce sync.Once
}

func (p *pesho) interruptHandler() {
	notifier := make(chan os.Signal)
	signal.Notify(notifier, os.Interrupt, syscall.SIGTERM)

	<-notifier

	log.Print("Received SIGINT/SIGTERM; Exiting gracefully...")

	p.Close()

	os.Exit(0)
}

func (p *pesho) Close() {
	p.closeOnce.Do(p.close)

}

func (p *pesho) close() {
	if p.door != nil {
		p.door.Close()
	}
}

func printDefaultConfig() {
	cfg, _ := config.LoadFromBytes(nil)
	data, _ := json.MarshalIndent(cfg, "", "    ")
	fmt.Print(string(data))
	os.Exit(0)
}

func maybeSubcommand() {
	if flag.NArg() == 0 {
		return
	}
	args := flag.Args()

	switch args[0] {
	case "printconfig":
		printDefaultConfig()
	}
}

func main() {

	runtime.GOMAXPROCS(1)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	flag.Parse()

	maybeSubcommand()

	cfg := config.LoadConfig(*configPath)

	log.Printf("config:\n%#v", *cfg)
	log.Printf("PID: %d", os.Getpid())

	// The hardware link
	d, err := door.NewFromConfig(cfg.Door)
	if err != nil {
		log.Fatalf("Could not init GPIOs: %v", err)
	}

	die := make(chan os.Signal)
	signal.Notify(die, os.Interrupt, syscall.SIGTERM)

	toggle := make(chan os.Signal)
	signal.Notify(toggle, syscall.SIGHUP)

	go ServeForever(d, cfg.Web)

	// just some demo code, listen to events and display them!
	doorEvents := d.Subscribe(nil)
	for err == nil {
		select {
		case evt := <-doorEvents:
			log.Printf("Now %s, was %s at %s\n", evt.New.String(), evt.Old.String(), evt.When)
		case <-die:
			log.Print("\nShutting down...\n")
			return
		case <-toggle:
			state := d.State()
			if state.IsLocked() {
				log.Print("Trying Unlock...")
				go func() {
					var err = d.Unlock()
					if err != nil {
						log.Printf("Unlock: %v", err)
					}
				}()
			} else {
				log.Print("Trying Lock...")
				go func() {
					var err = d.Lock()
					if err != nil {
						log.Printf("Lock: %v", err)
					}
				}()
			}
		}
	}

	// TODO: implement a governor for the events

	// TODO: implement the web service
}
