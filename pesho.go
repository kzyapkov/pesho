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
)

var configPath = flag.String("config", "/etc/pesho/config.json", "Configuration file")

type pesho struct {
	door      Door
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

func config(filename string) (cfg *Config) {
	var err error
	cfg, err = LoadConfigFromFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			log.Print("File '%s' does not exist, using default configuration")
			return
		}
		log.Panicf("Config file could not be parsed: %v", err)
	}
	return cfg
}

func printDefaultConfig() {
	cfg, _ := LoadConfig(nil)
	data, _ := json.MarshalIndent(cfg, "", "    ")
	fmt.Print(string(data))
}

func main() {

	runtime.GOMAXPROCS(1)

	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if flag.NArg() > 0 {
		args := flag.Args()
		if args[0] == "printconfig" {
			printDefaultConfig()
			os.Exit(0)
		}
	}

	cfg := config(*configPath)

	// The hardware link
	d, err := NewDoor(cfg.Door)
	if err != nil {
		log.Panicf("Could not init GPIOs: %v", err)
	}

	irq := make(chan os.Signal)
	signal.Notify(irq, os.Interrupt, syscall.SIGTERM)

	go ServeForever(d, cfg.Web)

	// just some demo code, listen to events and display them!
	doorEvents := d.Subscribe(nil)
	for err == nil {
		select {
		case evt := <-doorEvents:
			log.Printf("locked: %t, closed: %t at %v\n", evt.Locked, evt.Closed, evt.When)
		case <-irq:
			log.Print("\nShutting down...\n")
			return
		}
	}

	// TODO: implement a governor for the events

	// TODO: implement the web service
}
