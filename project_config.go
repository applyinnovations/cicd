package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type EnvironmentProviderSource string

const (
	Phase EnvironmentProviderSource = "phase"
)

type EnvironmentProvider struct {
	source EnvironmentProviderSource `yaml:"type"`
	value  string                    `yaml:"value"`
}

type BranchConfig struct {
	environment EnvironmentProvider `yaml:"environment"`
}

type ProjectConfig map[string]BranchConfig

func (pr ProjectConfig) getBranchConfig(branch string) BranchConfig {
	if branchConfig, exists := pr[branch]; exists {
		return branchConfig
	} else if branchConfig, exists := pr["*"]; exists {
		return branchConfig
	}

	return BranchConfig{}
}

func getProjectConf(ctx Context) (projectConf ProjectConfig, err error) {
	projectConfig := ProjectConfig{}
	cacheDir := filepath.Join(CACHE_DIR, ctx.commitSha+"-"+ctx.defaultBranch)
	defer func() {
		err := os.RemoveAll(cacheDir)
		if err != nil {
			log.Printf("failed to remove `%s`\n", cacheDir)
		}
	}()

	err = execCmd("git", "clone", "--branch", ctx.defaultBranch, "--single-branch", ctx.repo, cacheDir)
	if err != nil {
		return projectConfig, fmt.Errorf("failed `git clone`: %w", err)
	}

	envPklFilePath := filepath.Join(cacheDir, "env.pkl")
	ymlPklFilePath := filepath.Join(cacheDir, "env.yml")

	err = execCmd("stat", envPklFilePath)
	if err != nil {
		return projectConfig, nil
	}

	err = execCmd("pkl", "eval", envPklFilePath, "--format", "yaml", "--output-path", ymlPklFilePath, "--property", "branch="+ctx.branch)

	if err != nil {
		return projectConfig, fmt.Errorf("failed `pkl eval`: %w", err)
	}
	out, err := os.ReadFile(ymlPklFilePath)
	if err != nil {
		return projectConfig, fmt.Errorf("failed to read %+v", err)
	}

	err = yaml.Unmarshal(out, &projectConfig)
	if err != nil {
		return projectConfig, fmt.Errorf("failed to unmarshal %+v", err)
	}

	return projectConfig, nil
}
