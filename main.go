package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/go-playground/webhooks/v6/github"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
)

const (
	WEBHOOK_PATH = "/webhooks"
	CACHE_PATH   = "cache"
)

type Context struct {
	repo          string
	branch        string
	repoSha       string
	branchSha     string
	commitSha     string
	deploymentSha string
}

func generateContext(repo, branch, commitSha string) Context {

	// generate inputs to handlers
	repoSha := sha256.Sum256([]byte(repo))
	branchSha := sha256.Sum256([]byte(branch))
	deploymentSha := sha256.Sum256([]byte(fmt.Sprintf("%s%s", repo, branch)))
	return Context{
		repo,
		branch,
		commitSha,
		string(repoSha[:]),
		string(branchSha[:]),
		string(deploymentSha[:]),
	}
}

// use when update to branch
func handleUp(ctx Context) error {

	// clone/pull to cache/repos/repo/docker-compose.yml
	cacheDir := filepath.Join(CACHE_PATH, ctx.repoSha, ctx.branchSha, ctx.commitSha)
	cloneCmd := exec.Command("git", "clone", "--branch", ctx.branch, "--single-branch", ctx.repo, cacheDir)
	cloneCmd.Stdout = log.Writer()
	cloneCmd.Stderr = log.Writer()
	if err := cloneCmd.Run(); err != nil {
		return fmt.Errorf("failed `git clone`: %w", err)
	}

	// validate docker compose file
	validateCmd := exec.Command("docker", "compose", "--project-directory", cacheDir, "config")
	validateCmd.Stdout = log.Writer()
	validateCmd.Stderr = log.Writer()
	if err := validateCmd.Run(); err != nil {
		return fmt.Errorf("failed `docker compose config`: %w", err)
	}

	// docker compose up -p sha256(org/repo/branch)
	upCmd := exec.Command("docker", "compose", "--project-directory", cacheDir, "--project-name", ctx.deploymentSha)
	upCmd.Stdout = log.Writer()
	upCmd.Stderr = log.Writer()
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("failed `docker compose up`: %w", err)
	}

	return nil
}

// use when branch is deleted or repo is deleted
func handleDown(ctx Context) error {

	// stop containers
	cmd := exec.Command("docker", "container", "stop", fmt.Sprintf("$(docker ps -q -f name=%s)", ctx.deploymentSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker container stop`: %w", err)
	}

	// rm containers
	cmd = exec.Command("docker", "container", "rm", fmt.Sprintf("$(docker ps -a -q -f name=%s)", ctx.deploymentSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker container rm`: %w", err)
	}

	// rm network
	cmd = exec.Command("docker", "network", "rm", fmt.Sprintf("$(docker network ls -q -f name=%s)", ctx.deploymentSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker network rm`: %w", err)
	}

	// rm volume
	cmd = exec.Command("docker", "volume", "rm", fmt.Sprintf("$(docker volume ls -q -f name=%s)", ctx.deploymentSha))
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed `docker volume rm`: %w", err)
	}

	return nil
}

func main() {
	l := log.New(os.Stderr)
	hook, _ := github.New(github.Options.Secret("?"))
	http.HandleFunc(WEBHOOK_PATH, func(w http.ResponseWriter, r *http.Request) {
		payload, err := hook.Parse(r, github.PushEvent)
		if err != nil {
			if err == github.ErrEventNotFound {
				// event out of scope
			}
		}
		switch payload.(type) {
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

		case github.ReleasePayload:
		//	release := payload.(github.ReleasePayload)
		case github.PullRequestPayload:
			//	pullRequest := payload.(github.PullRequestPayload)
		}
	})
	http.ListenAndServe(":3000", nil)
}
