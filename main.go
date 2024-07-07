package main

import (
	"fmt"
	"github.com/go-playground/webhooks/v6/github"
	"net/http"
)

const (
	path = "/webhooks"
)

func main() {
	hook, _ := github.New(github.Options.Secret("?"))
	http.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		payload, err := hook.Parse(r, github.PushEvent)
		if err != nil {
			if err == github.ErrEventNotFound {
				// event out of scope
			}
		}
		switch payload.(type) {
		case github.PushPayload:
			push := payload.(github.PushPayload)
			fmt.Println(push.Compare)
		case github.ReleasePayload:
		//	release := payload.(github.ReleasePayload)
		case github.PullRequestPayload:
			//	pullRequest := payload.(github.PullRequestPayload)
		}
	})
	http.ListenAndServe(":3000", nil)
}
