package main

import (
	"net/http"
)

type webService struct {
	door *Door
}

func handleOpen() {

}

func handleClose() {

}

func (ws *webService) ServeHTTP(w http.ResponseWriter, r *http.Request) {

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
