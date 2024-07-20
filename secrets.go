package main

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
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

func parseSecrets(ctx Context, filename string) ([]string, error) {
	secrets := new(bytes.Buffer)
	err := execCmd(secrets, "pkl", "eval", filename, "--format", "yaml", "--property", "branch="+ctx.branch)
	if err != nil {
		return nil, err
	}
	envVars := EnvironmentVariables{}
	err = yaml.Unmarshal(secrets.Bytes(), &envVars)
	var envArray []string
	for key, value := range envVars {
		if !validateKey(key) {
			return nil, fmt.Errorf("invalid env var key: %s", key)
		}
		envArray = append(envArray, fmt.Sprintf("%s=%s", key, escapeShellArg(value)))
	}
	return envArray, err
}
