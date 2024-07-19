package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/webhooks/v6/github"
)

const (
	WEBHOOK_PATH = "/webhooks"
	CACHE_DIR    = "/tmp"
	LOG_DIR      = "/log"
)

type Context struct {
	repo          string
	branch        string
	repoSha       string
	branchSha     string
	commitSha     string
	repoBranchSha string
}

func generateContext(repo, ref, commitSha string) Context {

	// generate inputs to handlers
	branch := strings.TrimPrefix(ref, "refs/heads/")
	repoSha := sha256.Sum256([]byte(repo))
	branchSha := sha256.Sum256([]byte(branch))
	repoBranchSha := sha256.Sum256([]byte(fmt.Sprintf("%s%s", repo, branch)))
	return Context{
		repo,
		branch,
		commitSha,
		hex.EncodeToString(repoSha[:]),
		hex.EncodeToString(branchSha[:]),
		hex.EncodeToString(repoBranchSha[:]),
	}
}

// use when update to branch
func handleUp(ctx Context) error {

	// clone/pull to cache/repos/repo/docker-compose.yml
	cacheDir := filepath.Join(CACHE_DIR, ctx.commitSha)

	defer func() {
		err := os.RemoveAll(cacheDir)
		if err != nil {
			log.Printf("failed to remove `%s`\n", cacheDir)
		}
	}()

	err := execCmd("git", "clone", "--branch", ctx.branch, "--single-branch", ctx.repo, cacheDir)
	if err != nil {
		return fmt.Errorf("failed `git clone`: %w", err)
	}

	pklFilePath := filepath.Join(cacheDir, "docker-compose.pkl")
	ymlFilePath := filepath.Join(cacheDir, "docker-compose.yml")

	err = execCmd("stat", pklFilePath)
	if err == nil {
		err = execCmd("pkl", "eval", pklFilePath, "--format", "yaml", "--output-path", ymlFilePath, "--property", "branch="+ctx.branch)
		if err != nil {
			return fmt.Errorf("failed `pkl eval`: %w", err)
		}
	}

	err = execCmd("stat", ymlFilePath)
	if err != nil {
		return fmt.Errorf("failed `stat docker-compose.yml`: %w", err)
	}

	// docker compose up -p sha256(org/repo/branch)
	err = execCmd("docker", "compose", "--project-directory", cacheDir, "--file", ymlFilePath, "--project-name", ctx.repoBranchSha, "up", "--quiet-pull", "--detach", "--build", "--remove-orphans")
	if err != nil {
		return fmt.Errorf("failed `docker compose up`: %w", err)
	}

	return nil
}

// use when branch is deleted or repo is deleted
func handleDown(ctx Context) error {

	// stop containers
	err := execCmd("docker", "container", "stop", fmt.Sprintf("$(docker ps -q -f name=%s)", ctx.repoBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker container stop`: %w", err)
	}

	// rm containers
	err = execCmd("docker", "container", "rm", fmt.Sprintf("$(docker ps -a -q -f name=%s)", ctx.repoBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker container rm`: %w", err)
	}

	// rm network
	err = execCmd("docker", "network", "rm", fmt.Sprintf("$(docker network ls -q -f name=%s)", ctx.repoBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker network rm`: %w", err)
	}

	// rm volume
	err = execCmd("docker", "volume", "rm", fmt.Sprintf("$(docker volume ls -q -f name=%s)", ctx.repoBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker volume rm`: %w", err)
	}

	return nil
}

func main() {
	err := os.MkdirAll(LOG_DIR, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create `%s` directory: %v", LOG_DIR, err))
	}

	jsonHandler := slog.NewJSONHandler(os.Stderr, nil)
	logger := slog.New(jsonHandler)
	slog.SetDefault(logger)

	err = os.MkdirAll(CACHE_DIR, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create `%s` directory: %v", CACHE_DIR, err))
	}

	hook, _ := github.New(github.Options.Secret("?"))
	http.HandleFunc(WEBHOOK_PATH, func(w http.ResponseWriter, r *http.Request) {
		payload, err := hook.Parse(r, github.PushEvent, github.DeleteEvent)
		switch payload := payload.(type) {
		case github.CreatePayload:
			// deploy latest
			if payload.RefType == "branch" {
				ctx := generateContext(payload.Repository.CloneURL, payload.Ref, "")
				err := handleUp(ctx)
				if err != nil {
					log.Println("failed `handleUp`: %w", err)
				}
			}

		case github.DeletePayload:
			// clean up releases
			if payload.RefType == "branch" {
				ctx := generateContext(payload.Repository.CloneURL, payload.Ref, "")
				err := handleDown(ctx)
				if err != nil {
					log.Println("failed `handleDown`: %w", err)
				}
			}

		case github.PushPayload:
			// deploy latest
			ctx := generateContext(payload.Repository.CloneURL, payload.Ref, payload.After)
			err := handleUp(ctx)
			if err != nil {
				log.Println("failed `handleUp`: %w", err)
			}
		}
		if err != nil {
			if err == github.ErrEventNotFound {
				// event out of scope
				log.Println("Event out of scope")
				w.WriteHeader(http.StatusNotImplemented)
				fmt.Fprintln(w, "{\"error\":\"not implemented\"}")
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "{\"error\":\"%v\"}", err)
			}
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "{}")
		}
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "")
	})
	http.ListenAndServe(":80", nil)
}
