package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/kzyapkov/pesho/config"
	"github.com/kzyapkov/pesho/door"
)

func setHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "pesho/0.1")
}

type response struct {
	State door.State `json:"state"`
	Error error      `json:"error,omitempty"`
}

func (r *response) WriteTo(w http.ResponseWriter) {
	d, _ := json.Marshal(r)
	w.Write(d)
}

func (p *pesho) handleLock(w http.ResponseWriter, r *http.Request) {
	setHeaders(w)
	var resp response
	resp.Error = p.door.Lock()
	resp.State = p.door.State()
	resp.WriteTo(w)
}

func (p *pesho) handleUnlock(w http.ResponseWriter, r *http.Request) {
	setHeaders(w)
	var resp response
	resp.Error = p.door.Unlock()
	resp.State = p.door.State()
	resp.WriteTo(w)
}

func (p *pesho) handleStatus(w http.ResponseWriter, r *http.Request) {
	setHeaders(w)
	data, err := json.MarshalIndent(p.door.State(), "", "    ")
	if err != nil {
		msg := fmt.Sprintf("Unable to read door state: %v", err)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func (p *pesho) httpServer(cfg config.WebConfig) {
	defer p.workers.Done()

	http.Handle("/status", restrictMethod(http.HandlerFunc(p.handleStatus), "GET"))
	http.Handle("/lock", restrictMethod(http.HandlerFunc(p.handleLock), "POST"))
	http.Handle("/unlock", restrictMethod(http.HandlerFunc(p.handleUnlock), "POST"))

	var err error
	// fix this to be a real server with control over the listener
	if cfg.Listen != "" {
		go func() {
			if err = http.ListenAndServe(cfg.Listen, nil); err != nil {
				log.Fatalf("web: %v", err)
			}
		}()
	}
	if cfg.TLS != nil {
		log.Print("TLS not yet implemented")
	}

	<-p.dying
}

func restrictMethod(h http.Handler, methods ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		found := false
		for _, m := range methods {
			if r.Method == m {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.ServeHTTP(w, r)
	})
}
