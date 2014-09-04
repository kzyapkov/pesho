package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type TLSConfig struct {
	Listen   string
	CertFile string
	KeyFile  string
}

type WebConfig struct {
	Listen string
	TLS    *TLSConfig
}

type PeshoWeb struct {
	door Door
}

func setHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "pesho/0.1")
}

func (p *PeshoWeb) handleOpen(w http.ResponseWriter, r *http.Request) {

}

func (p *PeshoWeb) handleClose(w http.ResponseWriter, r *http.Request) {

}

func (p *PeshoWeb) handleStatus(w http.ResponseWriter, r *http.Request) {
	setHeaders(w)
	data, err := json.MarshalIndent(p.door.State(), "", "    ")
	if err != nil {
		msg := fmt.Sprintf("Unable to read door state: %v", err)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func ServeForever(d Door, cfg WebConfig) {
	p := &PeshoWeb{door: d}
	http.Handle("/status", restrictMethod(http.HandlerFunc(p.handleStatus), "GET"))
	err := http.ListenAndServe(cfg.Listen, nil)
	if err != nil {
		log.Panicf("web: %v")
	}
	if cfg.TLS != nil {
		log.Print("TLS not yet implemented")
	}
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
		return
	})
}
