package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/go-playground/webhooks/v6/github"
	"gopkg.in/yaml.v3"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	WEBHOOK_PATH = "/webhooks"
	CACHE_DIR    = "/tmp"
	LOG_DIR      = "/log"
)

type Context struct {
	repo          string
	branch        string
	defaultBranch string
	repoSha       string
	branchSha     string
	commitSha     string
	repoBranchSha string
}

type ProjectConf struct {
	Phase map[string]string `yaml:"phase"`
}

func generateContext(repo, ref, defaultBranch, commitSha string) Context {

	// generate inputs to handlers
	branch := strings.TrimPrefix(ref, "refs/heads/")
	repoSha := sha256.Sum256([]byte(repo))
	branchSha := sha256.Sum256([]byte(branch))
	repoBranchSha := sha256.Sum256([]byte(fmt.Sprintf("%s%s", repo, branch)))
	return Context{
		repo,
		branch,
		defaultBranch,
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

func execCmd(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	return cmd.Run()
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
				ctx := generateContext(payload.Repository.CloneURL, payload.Ref, payload.Repository.DefaultBranch, "")
				err := handleUp(ctx)
				if err != nil {
					log.Println("failed `handleUp`: %w", err)
				}
			}

		case github.DeletePayload:
			// clean up releases
			if payload.RefType == "branch" {
				ctx := generateContext(payload.Repository.CloneURL, payload.Ref, payload.Repository.DefaultBranch, "")
				err := handleDown(ctx)
				if err != nil {
					log.Println("failed `handleDown`: %w", err)
				}
			}

		case github.PushPayload:
			// deploy latest
			ctx := generateContext(payload.Repository.CloneURL, payload.Ref, payload.Repository.DefaultBranch, payload.After)
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

func (c *ProjectConf) getPhaseEnv(ctx Context) string {
	// get value of branch in c.Phase if not exist use "*"
	environment := c.Phase[ctx.branch]
	if environment == "" {
		environment = c.Phase["*"]
	}
	return environment
}

func getProjectConf(ctx Context) (projectConf ProjectConf, err error) {
	c := ProjectConf{}

	cacheDir := filepath.Join(CACHE_DIR, ctx.commitSha+ctx.defaultBranch)
	defer func() {
		err := os.RemoveAll(cacheDir)
		if err != nil {
			log.Printf("failed to remove `%s`\n", cacheDir)
		}
	}()

	err = execCmd("git", "clone", "--branch", ctx.defaultBranch, "--single-branch", ctx.repo, cacheDir)
	if err != nil {
		return c, fmt.Errorf("failed `git clone`: %w", err)
	}

	envPklFilePath := filepath.Join(cacheDir, "env.pkl")
	ymlPklFilePath := filepath.Join(cacheDir, "env.yml")

	err = execCmd("stat", envPklFilePath)
	if err != nil {
		return c, nil
	}

	err = execCmd("pkl", "eval", envPklFilePath, "--format", "yaml", "--output-path", ymlPklFilePath, "--property", "branch="+ctx.branch)

	if err != nil {
		return c, fmt.Errorf("failed `pkl eval`: %w", err)
	}
	out, err := os.ReadFile(ymlPklFilePath)
	if err != nil {
		return c, fmt.Errorf("failed to read %+v", err)
	}

	err = yaml.Unmarshal(out, &c)
	if err != nil {
		return c, fmt.Errorf("failed to unmarshal %+v", err)
	}

	return c, nil
}
