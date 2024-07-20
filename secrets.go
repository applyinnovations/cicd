package main

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"path/filepath"
	"regexp"
	"strings"
)

type EnvironmentVariables map[string]string

func escapeShellArg(arg string) string {
	return `'` + strings.Replace(arg, `'`, `'\''`, -1) + `'`
}

func validateKey(key string) bool {
	// Only allow alphanumeric and underscore characters
	validKey := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	return validKey.MatchString(key)
}

func parseSecretsToEnvArray(ctx Context) ([]string, error) {
	envVars, err := parseSecretsToEnv(ctx)
	if err != nil {
		return nil, err
	}
	var envArray []string
	for key, value := range envVars {
		if !validateKey(key) {
			return nil, fmt.Errorf("invalid env var key: %s", key)
		}
		envArray = append(envArray, fmt.Sprintf("%s=%s", key, escapeShellArg(value)))
	}
	return envArray, nil
}

func parseSecretsToEnv(ctx Context) (EnvironmentVariables, error) {
	scrtFilePath := filepath.Join("/secrets", ctx.cloneUrlSha)
	err := execCmd(log.Writer(), "stat", scrtFilePath)
	if err != nil {
		return nil, err
	}
	secrets := new(bytes.Buffer)
	err = execCmd(secrets, "pkl", "eval", scrtFilePath, "--format", "yaml", "--property", "branch="+ctx.branch)
	if err != nil {
		return nil, err
	}
	envVars := EnvironmentVariables{}
	err = yaml.Unmarshal(secrets.Bytes(), &envVars)
	return envVars, err
}
