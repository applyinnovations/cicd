package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

func execLogCmd(out io.Writer, command string, args ...string) error {
	log.Printf("%s %+q", command, args)
	return execCmd(out, command, args...)
}

func execCmd(out io.Writer, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = out
	cmd.Stderr = log.Writer()
	return cmd.Run()
}

type Context struct {
	branch            string
	cloneUrl          string
	repository        string
	commitSha         string
	cloneUrlSha       string
	branchSha         string
	cloneUrlBranchSha string
}

func generateContext(cloneUrl, ref, commitSha, repository string) Context {
	// generate inputs to handlers
	branch := strings.TrimPrefix(ref, "refs/heads/")
	cloneUrlSha := sha256.Sum256([]byte(cloneUrl))
	branchSha := sha256.Sum256([]byte(branch))
	cloneUrlBranchSha := sha256.Sum256([]byte(fmt.Sprintf("%s%s", cloneUrl, branch)))
	return Context{
		branch,
		cloneUrl,
		repository,
		commitSha,
		hex.EncodeToString(cloneUrlSha[:]),
		hex.EncodeToString(branchSha[:]),
		hex.EncodeToString(cloneUrlBranchSha[:]),
	}
}

func addDozzleGroupLabel(ctx Context, filePath string) error {
	group := fmt.Sprintf("%s/%s", strings.ToLower(ctx.repository), strings.ToLower(ctx.branch))
	yamlFile, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read docker compose yaml: %w", err)
	}
	yamlData := make(map[string]interface{})
	err = yaml.Unmarshal(yamlFile, &yamlData)
	if err != nil {
		return fmt.Errorf("failed to unmarshal docker compose yaml: %w", err)
	}
	services, ok := yamlData["services"].(map[string]interface{})
	if !ok {
		// no services
		return nil
	}
	for serviceKey, service := range services {
		serviceMap, ok := service.(map[string]interface{})
		if !ok {
			continue
		}

		if labels, ok := serviceMap["labels"].(map[string]interface{}); ok {
			// add to the labels map
			labels["dev.dozzle.group"] = group
			labels["dev.dozzle.name"] = serviceKey
		} else if labelArray, ok := serviceMap["labels"].([]interface{}); ok {
			// add to the labels array
			serviceMap["labels"] = append(labelArray, fmt.Sprintf("dev.dozzle.group=%s", group), fmt.Sprintf("dev.dozzle.name=%s", serviceKey))
		} else {
			// create a new labels entry
			serviceMap["labels"] = map[string]string{
				"dev.dozzle.group": group,
				"dev.dozzle.name":  serviceKey,
			}
		}
	}
	// write new docker compose to file
	newYamlData, err := yaml.Marshal(yamlData)
	if err != nil {
		return fmt.Errorf("failed to marshal updated docker compose yaml: %w", err)
	}
	err = os.WriteFile(filePath, newYamlData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write updated docker compose yaml: %w", err)
	}
	return nil
}
