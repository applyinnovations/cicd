package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProjectConf struct {
	Phase map[string]string `yaml:"phase"`
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
	cacheDir := filepath.Join(CACHE_DIR, ctx.commitSha+"-"+ctx.defaultBranch)
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
