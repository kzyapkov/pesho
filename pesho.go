// pesho, named after Peter the Saint
// is the gatekeeper of the premises of initlab.org
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
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
	closeOnce *sync.Once
	dying     chan struct{}
	workers   sync.WaitGroup
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
	close(p.dying)
	if p.door != nil {
		p.door.Close()
	}
}

func printDefaultConfig() {
	cfg, _ := config.LoadFromBytes(nil)
	data, _ := json.MarshalIndent(cfg, "", "    ")
	fmt.Print(string(data))
	fmt.Print("\n")
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

func (p *pesho) stateMonitor() {
	defer p.workers.Done()

	doorEvents := p.door.Subscribe(nil)
	defer p.door.Unsubscribe(doorEvents)
	for {
		select {
		case <-p.dying:
			return
		case evt := <-doorEvents:
			log.Printf("Now %s, was %s at %s\n", evt.New.String(), evt.Old.String(), evt.When)
		}
	}
}

func (p *pesho) hangupHandler() {
	defer p.workers.Done()
	toggle := make(chan os.Signal)
	signal.Notify(toggle, syscall.SIGHUP)
	for {
		select {
		case <-p.dying:
			return
		case <-toggle:
			state := p.door.State()
			log.Printf("SIGHUP: %v", state)
			if state.IsLocked() {
				log.Print("Trying Unlock...")
				go func() {
					var err = p.door.Unlock()
					if err != nil {
						log.Printf("Unlock: %v", err)
					}
				}()
			} else {
				log.Print("Trying Lock...")
				go func() {
					var err = p.door.Lock()
					if err != nil {
						log.Printf("Lock: %v", err)
					}
				}()
			}
		}
	}
}

func (p *pesho) buttonHandler(btns *buttons) {
	defer p.workers.Done()
	for {
		select {
		case <-p.dying:
			return
		case b := <-btns.Presses:
			if b == RedButton {
				log.Print("Red button pressed, locking")
				p.door.Lock()
			} else {
				log.Print("Green button pressed, locking")
				p.door.Unlock()
			}
		}
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
	ioutil.WriteFile("/var/run/pesho.pid", []byte(fmt.Sprintf("%d", os.Getpid())), 0644)

	d, err := door.NewFromConfig(cfg.Door)
	if err != nil {
		log.Fatalf("Could not init door GPIOs: %v", err)
	}
	b, err := newButtons(cfg.Buttons.Red, cfg.Buttons.Green)
	if err != nil {
		log.Fatalf("Could not init button GPIOs: %v", err)
	}
	p := &pesho{
		door:      d,
		closeOnce: &sync.Once{},
		dying:     make(chan struct{}),
	}

	go p.interruptHandler()

	p.workers.Add(1)
	go p.httpServer(cfg.Web)

	p.workers.Add(1)
	go p.stateMonitor()

	p.workers.Add(1)
	go p.hangupHandler()

	p.workers.Add(1)
	go p.buttonHandler(b)

	p.workers.Wait()
}
