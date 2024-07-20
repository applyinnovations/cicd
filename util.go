package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
)

func execCmd(out io.Writer, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = out
	cmd.Stderr = log.Writer()
	return cmd.Run()
}

type Context struct {
	branch            string
	cloneUrl          string
	commitSha         string
	cloneUrlSha       string
	branchSha         string
	cloneUrlBranchSha string
}

func generateContext(cloneUrl, ref, commitSha string) Context {
	// generate inputs to handlers
	branch := strings.TrimPrefix(ref, "refs/heads/")
	cloneUrlSha := sha256.Sum256([]byte(cloneUrl))
	branchSha := sha256.Sum256([]byte(branch))
	cloneUrlBranchSha := sha256.Sum256([]byte(fmt.Sprintf("%s%s", cloneUrl, branch)))
	return Context{
		branch,
		cloneUrl,
		commitSha,
		hex.EncodeToString(cloneUrlSha[:]),
		hex.EncodeToString(branchSha[:]),
		hex.EncodeToString(cloneUrlBranchSha[:]),
	}
}
