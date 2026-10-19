package daemon

import (
	"context"
	"os"
	"os/exec"

	"urgit/runner/internal/ship"
)

// Checkout still reads the operator-configured controller with its scoped
// read token. Automatic source metadata must not activate ambient Git
// filters/hooks, credentials, URL rewrites, proxies or protocol helpers.
// Redirects and recursive submodules are disabled. No YAML URL is used here.
func checkoutCommand(ctx context.Context, a *ship.Assignment, argv []string) *exec.Cmd {
	if a.Manifest == nil || a.Manifest.Mode != "resolve-tag" {
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		return cmd
	}
	args := []string{
		"-c", "core.hooksPath=/dev/null", "-c", "init.templateDir=",
		"-c", "core.fsmonitor=false", "-c", "credential.helper=",
		"-c", "http.proxy=", "-c", "http.followRedirects=false",
		"-c", "protocol.allow=never", "-c", "protocol.http.allow=always",
		"-c", "protocol.https.allow=always", "-c", "submodule.recurse=false",
	}
	cmd := exec.CommandContext(ctx, argv[0], append(args, argv[1:]...)...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_COUNT=0", "GIT_TERMINAL_PROMPT=0", "GIT_ATTR_NOSYSTEM=1",
	}
	return cmd
}
