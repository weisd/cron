package cron

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type CronHTTP struct {
	c *Cron
}

func NewCronHTTP(c *Cron) *CronHTTP {
	return &CronHTTP{c: c}
}

func (p *CronHTTP) Handler() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/c/job/list", func(w http.ResponseWriter, r *http.Request) {

		entries := p.c.Entries()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		err := json.NewEncoder(w).Encode(entries)
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(err.Error()))
			return
		}
	}).Methods("GET")

	r.HandleFunc("/c/job/log", func(w http.ResponseWriter, r *http.Request) {

		id, err := strconv.Atoi(r.FormValue("id"))
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(err.Error()))
			return
		}

		if id == 0 {
			w.WriteHeader(404)
			return
		}

		e := p.c.Entry(EntryID(id))
		if e.ID == 0 {
			w.WriteHeader(404)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		err = json.NewEncoder(w).Encode(e.Logs)
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(err.Error()))
			return
		}

	}).Methods("GET")

	r.HandleFunc("/c/job/pause", func(w http.ResponseWriter, r *http.Request) {

		id, err := strconv.Atoi(r.FormValue("id"))
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(err.Error()))
			return
		}

		if id == 0 {
			w.WriteHeader(404)
			return
		}

		p.c.PauseEntry(EntryID(id))
		w.WriteHeader(200)

	}).Methods("POST")

	r.HandleFunc("/c/job/start", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.FormValue("id"))
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(err.Error()))
			return
		}

		if id == 0 {
			w.WriteHeader(404)
			return
		}

		p.c.StartEntry(EntryID(id))
		w.WriteHeader(200)

	}).Methods("POST")

	r.HandleFunc("/c/job/run", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.FormValue("id"))
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte(err.Error()))
			return
		}

		if id == 0 {
			w.WriteHeader(404)
			return
		}

		p.c.RunEntry(EntryID(id))

		w.WriteHeader(200)

	}).Methods("POST")

	return r
}
