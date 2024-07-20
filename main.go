package main

import (
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/go-playground/webhooks/v6/github"
)

const (
	WEBHOOK_PATH = "/webhooks"
	CACHE_DIR    = "/tmp"
	LOG_DIR      = "/log"
	SECRETS_DIR  = "/secrets"
)

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

	err := execCmd(log.Writer(), "git", "clone", "--branch", ctx.branch, "--single-branch", ctx.cloneUrl, cacheDir)
	if err != nil {
		return fmt.Errorf("failed `git clone`: %w", err)
	}

	pklFilePath := filepath.Join(cacheDir, "docker-compose.pkl")
	ymlFilePath := filepath.Join(cacheDir, "docker-compose.yml")

	err = execCmd(log.Writer(), "stat", pklFilePath)
	if err == nil {
		err = execCmd(log.Writer(), "pkl", "eval", pklFilePath, "--format", "yaml", "--output-path", ymlFilePath, "--property", "branch="+ctx.branch)
		if err != nil {
			return fmt.Errorf("failed `pkl eval docker-compose.yml`: %w", err)
		}
	}

	err = execCmd(log.Writer(), "stat", ymlFilePath)
	if err != nil {
		return fmt.Errorf("failed `stat docker-compose.yml`: %w", err)
	}

	var secrets []string
	scrtFilePath := filepath.Join("/secrets", ctx.cloneUrlSha)
	err = execCmd(log.Writer(), "stat", scrtFilePath)
	if err == nil {
		// if secrets exists, then build it with the props
		secrets, err = parseSecrets(ctx, scrtFilePath)
		if err != nil {
			return fmt.Errorf("failed `parseSecrets`: %w", err)
		}
	}

	cmd := exec.Command("docker", "compose", "--project-directory", cacheDir, "--file", ymlFilePath, "--project-name", ctx.cloneUrlBranchSha, "up", "--quiet-pull", "--detach", "--build", "--remove-orphans")
	cmd.Stdout = log.Writer()
	cmd.Stderr = log.Writer()
	cmd.Env = secrets

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed `docker compose up`: %w", err)
	}

	return nil
}

// use when branch is deleted or repo is deleted
func handleDown(ctx Context) error {

	// stop containers
	err := execCmd(log.Writer(), "docker", "container", "stop", fmt.Sprintf("$(docker ps -q -f name=%s)", ctx.cloneUrlBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker container stop`: %w", err)
	}

	// rm containers
	err = execCmd(log.Writer(), "docker", "container", "rm", fmt.Sprintf("$(docker ps -a -q -f name=%s)", ctx.cloneUrlBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker container rm`: %w", err)
	}

	// rm network
	err = execCmd(log.Writer(), "docker", "network", "rm", fmt.Sprintf("$(docker network ls -q -f name=%s)", ctx.cloneUrlBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker network rm`: %w", err)
	}

	// rm volume
	err = execCmd(log.Writer(), "docker", "volume", "rm", fmt.Sprintf("$(docker volume ls -q -f name=%s)", ctx.cloneUrlBranchSha))
	if err != nil {
		return fmt.Errorf("failed `docker volume rm`: %w", err)
	}

	return nil
}

func handleSecretUpload(w http.ResponseWriter, r *http.Request) {
	// check method is post
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	// get cloneurl from form
	cloneurl := r.FormValue("url")
	if cloneurl == "" {
		http.Error(w, "clone url is missing", http.StatusBadRequest)
		return
	}
	// get file from form
	secret, header, err := r.FormFile("secret")
	if err != nil {
		http.Error(w, "secret is missing", http.StatusBadRequest)
		return
	}
	if filepath.Ext(header.Filename) != ".pkl" {
		http.Error(w, "only .pkl files are allowed", http.StatusBadRequest)
		return
	}
	ctx := generateContext(cloneurl, "", "")
	// store file in /secrets/repoSha.pkl
	os.MkdirAll(SECRETS_DIR, os.ModePerm)
	filename := filepath.Join(SECRETS_DIR, ctx.cloneUrlSha)
	out, err := os.Create(filename)
	if err != nil {
		log.Println("failed to create secret file: %w", err)
		http.Error(w, "failed to create secret.pkl", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	_, err = out.ReadFrom(secret)
	if err != nil {
		log.Println("failed to write secret file: %w", err)
		http.Error(w, "failed to write secret.pkl", http.StatusInternalServerError)
		return
	}
	// pkl eval file contents
	err = execCmd(log.Writer(), "pkl", "eval", filename, "--format", "yaml", "--property", "branch="+ctx.branch)
	if err != nil {
		log.Println("failed to evaluate secret file: %w", err)
		http.Error(w, "failed to evaluate secret.pkl", http.StatusBadRequest)
		err = os.Remove(filename)
		if err != nil {
			log.Printf("failed `os.Remove(%s)`: %v\n", filename, err)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func handleSecretUploadPage(w http.ResponseWriter, r *http.Request) {
	// check method is get
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	// return html form
	html := `
<!DOCTYPE html>
<html>
	<head>
		<title>Upload secrets.pkl</title>
		<style>
			* {
				font-family: monospace;
				color: #fff;
				border: none;
			}
			html, body {
			    display: flex;
			    flex-direction: column;
			    align-items: center;
				background-color: #111;
			}
			form {
				display: flex;
			    flex-direction: column;
			    gap: 10px;
			    flex: 1;
			    width: 500px;
			}
			input {
				background-color: #555;
			    padding: 10px;
			}
		</style>
	</head>
	<body>
		<h1>upload secrets.pkl file</h1>
		<form id="secretform" enctype="multipart/form-data" action="/secrets/upload" method="post">
			<label for="url">clone url</label>
			<input type="text" id="url" name="url" placeholder="https://github.com/account/repository.git" required />
			<label for="secret">secrets.pkl</label>
			<input type="file" id="secret" name="secret" required />
			<input type="submit" value="upload" />
		</form>
	</body>
</html>
`
	t, err := template.New("upload").Parse(html)
	if err == nil {
		err = t.Execute(w, nil)
	}
	if err != nil {
		log.Println("failed `handleSecretUploadPage`: %w", err)
	}
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

		if err != nil {
			if err == github.ErrEventNotFound {
				// event out of scope
				http.Error(w, "not implemented", http.StatusNotImplemented)

			} else {
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "ok")
		}

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

	})
	http.HandleFunc("/secrets/upload", handleSecretUpload)
	http.HandleFunc("/secrets", handleSecretUploadPage)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	http.ListenAndServe(":80", nil)
}
