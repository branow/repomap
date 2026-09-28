// Package git is the git command line, and nothing above it. Every call reads an
// existing checkout except Clone and Update, the only two that touch a network.
//
// It shells out rather than linking a git implementation: the machine running a
// build already has the credential helper, the proxy and the SSH keys that reach
// the forge, and reimplementing those is how a tool grows its own broken half of
// an authentication story.
package git

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// Runner runs one git invocation. It is an interface so every package above
// this one is testable without a repository on disk.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// CLI runs the real git binary.
type CLI struct {
	// Binary defaults to "git" on PATH.
	Binary string
}

// Run executes git in dir and returns its trimmed standard output.
func (c CLI) Run(ctx context.Context, dir string, args ...string) (string, error) {
	binary := c.Binary
	if binary == "" {
		binary = "git"
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	// Nothing this tool runs may stop for a passphrase or a username: a build
	// runs unattended, and a git that prompts hangs it forever.
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GCM_INTERACTIVE=never",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &CommandError{
			Args:   args,
			Dir:    dir,
			Stderr: strings.TrimSpace(stderr.String()),
			Err:    err,
		}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Available reports whether the git binary can be found and run at all, so a
// missing git is one clear message rather than a failure per repository.
func (c CLI) Available(ctx context.Context) error {
	if _, err := c.Run(ctx, "", "--version"); err != nil {
		return &MissingError{Err: err}
	}
	return nil
}

// Head is the commit the working tree is on.
func Head(ctx context.Context, r Runner, dir string) (string, error) {
	return r.Run(ctx, dir, "rev-parse", "HEAD")
}

// Branch is the checked-out branch name, or empty on a detached head.
func Branch(ctx context.Context, r Runner, dir string) (string, error) {
	name, err := r.Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || name == "HEAD" {
		return "", err
	}
	return name, nil
}

// LastCommit is the committer date of HEAD as an ISO date, or empty when the
// repository has no commits yet.
func LastCommit(ctx context.Context, r Runner, dir string) (string, error) {
	return r.Run(ctx, dir, "log", "-1", "--format=%cs")
}

// Files lists the tracked paths of the working tree, which is deliberately not
// the same as walking the directory: an index that carried build output and
// vendored dependencies would be mostly noise, and .gitignore already says
// which is which.
func Files(ctx context.Context, r Runner, dir string) ([]string, error) {
	out, err := r.Run(ctx, dir, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, path := range strings.Split(out, "\x00") {
		if path != "" {
			files = append(files, path)
		}
	}
	return files, nil
}

// Clone makes a shallow single-branch clone. Shallow because the index reads
// the tip and nothing else: a full history is minutes and gigabytes bought for
// a question nobody asks of this tool.
func Clone(ctx context.Context, r Runner, remote, dir, branch string) error {
	args := []string{"clone", "--depth=1", "--single-branch", "--no-tags"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, remote, dir)
	_, err := r.Run(ctx, "", args...)
	return err
}

// Update fast-forwards an existing shallow clone to the remote tip. It resets
// rather than merges: the clone is a cache this tool owns, so a local change in
// it is damage to repair, not work to preserve.
func Update(ctx context.Context, r Runner, dir string) error {
	if _, err := r.Run(ctx, dir, "fetch", "--depth=1", "--no-tags", "origin"); err != nil {
		return err
	}
	head, err := r.Run(ctx, dir, "rev-parse", "--abbrev-ref", "origin/HEAD")
	if err != nil || head == "" {
		// A clone made with --single-branch has no origin/HEAD; FETCH_HEAD is
		// what the fetch above just wrote, and is the same commit.
		head = "FETCH_HEAD"
	}
	_, err = r.Run(ctx, dir, "reset", "--hard", head)
	return err
}

// Remote is the fetch URL of origin, used to tell which repository a directory
// found on disk actually is.
func Remote(ctx context.Context, r Runner, dir string) (string, error) {
	return r.Run(ctx, dir, "remote", "get-url", "origin")
}

// IsRepo reports whether dir is the root of a git working tree.
func IsRepo(ctx context.Context, r Runner, dir string) bool {
	out, err := r.Run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// Reachable probes a remote without cloning it, so a whole run can fail once,
// up front, with one message about access instead of once per repository.
func Reachable(ctx context.Context, r Runner, remote string) error {
	_, err := r.Run(ctx, "", "ls-remote", "--heads", "--", remote)
	return err
}

// CommandError reports a git invocation that failed, carrying git's own
// diagnosis rather than a rewritten one.
type CommandError struct {
	Args   []string
	Dir    string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	message := "git " + strings.Join(e.Args, " ") + " failed"
	if e.Dir != "" {
		message += " in " + e.Dir
	}
	if e.Stderr != "" {
		return message + ": " + e.Stderr
	}
	return message + ": " + e.Err.Error()
}

func (e *CommandError) Unwrap() error { return e.Err }

// MissingError reports that git itself could not be run.
type MissingError struct{ Err error }

func (e *MissingError) Error() string {
	return "git is not installed or not on PATH: this tool reads repositories through it"
}

func (e *MissingError) Unwrap() error { return e.Err }
