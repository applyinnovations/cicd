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

func generateContext(repo, ref, commitSha string) Context {
	// generate inputs to handlers
	branch := strings.TrimPrefix(ref, "refs/heads/")
	repoSha := sha256.Sum256([]byte(repo))
	branchSha := sha256.Sum256([]byte(branch))
	repoBranchSha := sha256.Sum256([]byte(fmt.Sprintf("%s%s", repo, branch)))
	return Context{
		repo,
		branch,
		commitSha,
		hex.EncodeToString(repoSha[:]),
		hex.EncodeToString(branchSha[:]),
		hex.EncodeToString(repoBranchSha[:]),
	}
}
