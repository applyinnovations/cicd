package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/go-playground/webhooks/v6/github"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	WEBHOOK_PATH = "/webhooks"
	CACHE_DIR    = "cache"
	LOG_DIR      = "log"
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
	cacheDir := filepath.Join(CACHE_DIR, ctx.repoSha, ctx.branchSha, ctx.commitSha)

	defer func() {
		err := os.RemoveAll(cacheDir)
		if err != nil {
			log.Printf("failed to remove `%s`\n", cacheDir)
		}
	}()

	cmd := exec.Command("git", "clone", "--branch", ctx.branch, "--depth", "1", "--single-branch", ctx.repo, cacheDir)
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `git clone`: %w", err)
	}

	// validate docker compose file
	cmd = exec.Command("docker", "compose", "--project-directory", cacheDir, "config", "--quiet")
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker compose config`: %w", err)
	}

	// docker compose up -p sha256(org/repo/branch)
	cmd = exec.Command("docker", "compose", "--project-directory", cacheDir, "--project-name", ctx.repoBranchSha, "up", "--quiet-pull", "--detach")
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker compose up`: %w", err)
	}

	return nil
}

// use when branch is deleted or repo is deleted
func handleDown(ctx Context) error {

	// stop containers
	cmd := exec.Command("docker", "container", "stop", fmt.Sprintf("$(docker ps -q -f name=%s)", ctx.repoBranchSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker container stop`: %w", err)
	}

	// rm containers
	cmd = exec.Command("docker", "container", "rm", fmt.Sprintf("$(docker ps -a -q -f name=%s)", ctx.repoBranchSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker container rm`: %w", err)
	}

	// rm network
	cmd = exec.Command("docker", "network", "rm", fmt.Sprintf("$(docker network ls -q -f name=%s)", ctx.repoBranchSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker network rm`: %w", err)
	}

	// rm volume
	cmd = exec.Command("docker", "volume", "rm", fmt.Sprintf("$(docker volume ls -q -f name=%s)", ctx.repoBranchSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker volume rm`: %w", err)
	}

	return nil
}

func main() {
	err := os.MkdirAll(LOG_DIR, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create `%s` directory: %v", LOG_DIR, err))
	}

	logFilename := fmt.Sprintf("%s.log", time.Now().Format("20060102-150405"))
	logFile, err := os.OpenFile(filepath.Join(LOG_DIR, logFilename), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(fmt.Sprintf("failed to open log file `%s`: %v", logFilename, err))
	}
	defer logFile.Close()

	jsonHandler := slog.NewJSONHandler(logFile, nil)
	logger := slog.New(jsonHandler)
	slog.SetDefault(logger)

	err = os.MkdirAll(CACHE_DIR, 0755)
	if err != nil {
		panic(fmt.Sprintf("failed to create `%s` directory: %v", CACHE_DIR, err))
	}

	hook, _ := github.New(github.Options.Secret("?"))
	http.HandleFunc(WEBHOOK_PATH, func(w http.ResponseWriter, r *http.Request) {
		payload, err := hook.Parse(r, github.PushEvent, github.DeleteEvent)
		if err != nil {
			if err == github.ErrEventNotFound {
				// event out of scope
			}
		}
		switch payload.(type) {

		case github.CreatePayload:

			// deploy latest
			create := payload.(github.CreatePayload)
			if create.RefType == "branch" {
				repo := create.Repository.CloneURL
				branch := create.Ref
				ctx := generateContext(repo, branch, "")
				err := handleUp(ctx)
				if err != nil {
					log.Println("failed `handleUp`: %w", err)
				}
			}

		case github.PushPayload:

			// deploy latest
			push := payload.(github.PushPayload)
			repo := push.Repository.CloneURL
			branch := push.Ref
			commitSha := push.After
			ctx := generateContext(repo, branch, commitSha)
			err := handleUp(ctx)
			if err != nil {
				log.Println("failed `handleUp`: %w", err)
			}

		case github.DeletePayload:

			// clean up releases
			del := payload.(github.DeletePayload)
			if del.RefType == "branch" {
				repo := del.Repository.CloneURL
				branch := del.Ref
				ctx := generateContext(repo, branch, "")
				err := handleDown(ctx)
				if err != nil {
					log.Println("failed `handleDown`: %w", err)
				}
			}
		}
	})
	http.ListenAndServe(":3000", nil)
}
