package main

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/weisd/cron/v4"
)

func main() {

	c := cron.New()

	var done int

	c.AddFunc("test", "@every 1s", func(context.Context) error {

		done++

		if done%3 == 0 {
			return errors.New("test fail")
		}

		log.Println("do test done")
		return nil
	})

	c.Start(context.TODO())

	h := cron.NewCronHTTP(c)

	http.Handle("/", h.Handler())

	addr := ":8989"

	log.Println("start http on ", addr)

	err := http.ListenAndServe(addr, nil)
	if err != nil {
		log.Panic(err)
	}

}
