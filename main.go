package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/go-github/v63/github"
	"github.com/jferrl/go-githubauth"
	"golang.org/x/oauth2"
)

const (
	CACHE_DIR   = "/tmp"
	LOG_DIR     = "/log"
	SECRETS_DIR = "/secrets"
)

// use when update to branch
func handleUp(ctx Context, tokenSource oauth2.TokenSource) error {

	// clone/pull to cache/repos/repo/docker-compose.yml
	cacheDir := filepath.Join(CACHE_DIR, ctx.commitSha)

	token, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to get token from app integration: %w", err)
	}

	defer func() {
		err := os.RemoveAll(cacheDir)
		if err != nil {
			log.Printf("failed to remove `%s`\n", cacheDir)
		}
	}()

	parsedCloneUrl, err := url.Parse(ctx.cloneUrl)
	if err != nil {
		return fmt.Errorf("failed to parse cloneUrl")
	}
	parsedCloneUrl.User = url.UserPassword("x-access-token", token.AccessToken)

	err = execCmd(log.Writer(), "git", "clone", "--branch", ctx.branch, "--single-branch", parsedCloneUrl.String(), cacheDir)
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
		secrets, err = parseSecretsToEnvArray(ctx)
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

func getResourceIds(ctx Context, args ...string) ([]string, error) {
	// list resources
	ids := new(bytes.Buffer)
	extraArgs := []string{"--quiet", "--filter", fmt.Sprintf("name=%s-*", ctx.cloneUrlBranchSha)}
	err := execCmd(ids, "docker", append(args, extraArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("failed `docker container ls`: %w", err)
	}
	return strings.Split(ids.String(), "\n"), nil
}

// use when branch is deleted or repo is deleted
func handleDown(ctx Context) error {

	// list running containers
	runningContainerIds, err := getResourceIds(ctx, "container", "ls")
	if err != nil {
		return fmt.Errorf("failed `docker container ls`: %w", err)
	}

	// stop containers
	err = execCmd(log.Writer(), "docker", append([]string{"container", "stop"}, runningContainerIds...)...)
	if err != nil {
		return fmt.Errorf("failed `docker container stop`: %w", err)
	}

	// list all matching containers
	containerIds, err := getResourceIds(ctx, "container", "ls", "--all")
	if err != nil {
		return fmt.Errorf("failed `docker container ls --all`: %w", err)
	}

	// rm containers
	err = execCmd(log.Writer(), "docker", append([]string{"container", "rm"}, containerIds...)...)
	if err != nil {
		return fmt.Errorf("failed `docker container rm`: %w", err)
	}

	// list all matching containers
	networkIds, err := getResourceIds(ctx, "network", "ls")
	if err != nil {
		return fmt.Errorf("failed `docker network ls`: %w", err)
	}

	// rm network
	err = execCmd(log.Writer(), "docker", append([]string{"network", "rm"}, networkIds...)...)
	if err != nil {
		return fmt.Errorf("failed `docker network rm`: %w", err)
	}

	// list all matching containers
	volumeIds, err := getResourceIds(ctx, "volume", "ls")
	if err != nil {
		return fmt.Errorf("failed `docker volume ls`: %w", err)
	}

	// rm volume
	err = execCmd(log.Writer(), "docker", append([]string{"volume", "rm"}, volumeIds...)...)
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
	err = execCmd(nil, "pkl", "eval", filename, "--format", "yaml", "--property", "branch="+ctx.branch)
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
		<h1>upload repository secrets.pkl</h1>
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

func buildHandleWebhook() http.HandlerFunc {
	cicdCtx := generateContext("https://github.com/applyinnovations/cicd.git", "refs/heads/main", "")
	secrets, err := parseSecretsToEnv(cicdCtx)
	if err != nil {
		log.Println("failed to parse cicd secrets")
		return func(w http.ResponseWriter, r *http.Request) {}
	}
	githubAppId, err := strconv.ParseInt(secrets["GITHUB_APP_ID"], 10, 64)
	if err != nil {
		log.Println("failed to parse git app id")
		return func(w http.ResponseWriter, r *http.Request) {}
	}
	githubPrivateKey := []byte(secrets["GITHUB_PRIVATE_KEY"])
	appTokenSource, err := githubauth.NewApplicationTokenSource(githubAppId, githubPrivateKey)

	if err != nil {
		log.Println("failed `githubauth.NewApplicationTokenSource`: %w", err)
		return func(w http.ResponseWriter, r *http.Request) {}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := github.ValidatePayload(r, []byte("?"))
		if err != nil {
			log.Println("failed `github.ValidatePayload`: %w", err)
			return
		}
		event, err := github.ParseWebHook(github.WebHookType(r), payload)
		if err != nil {
			log.Println("failed `github.ParseWebHook`: %w", err)
			return
		}
		switch event := event.(type) {
		case *github.PushEvent:
			// deploy latest
			ctx := generateContext(event.GetRepo().GetCloneURL(), event.GetRef(), event.GetAfter())
			if !event.GetDeleted() {
				installationTokenSource := githubauth.NewInstallationTokenSource(event.GetInstallation().GetID(), appTokenSource)
				err = handleUp(ctx, installationTokenSource)
				if err != nil {
					log.Println("failed `handleUp`: %w", err)
				}
			} else {
				err := handleDown(ctx)
				if err != nil {
					log.Println("failed `handleDown`: %w", err)
				}
			}
			return
		}
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

	http.HandleFunc("/webhooks", buildHandleWebhook())
	http.HandleFunc("/secrets/upload", handleSecretUpload)
	http.HandleFunc("/secrets", handleSecretUploadPage)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	http.ListenAndServe(":80", nil)
}
