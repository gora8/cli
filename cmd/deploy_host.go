package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gora8/cli/internal/api"
	"github.com/gora8/cli/internal/ui"
)

var (
	deployHost       bool
	deployHostImage  string
	deployHostPort   int
	deployHostCPU    int
	deployHostMemory int
)

// runHostFlow is called from runDeploy after the agent is registered
// (agentID is real) — Pro-tier hosted compute (see
// docs/hosting/gora8-managed-fargate.md), the CLI-facing half of the
// work done alongside it. Deliberately not a build pipeline: if
// --host-image is given, that's used as-is (bring your own registry,
// zero Docker interaction here at all); --host alone means "build the
// Dockerfile at this path and push it to a gora8-managed repo gora8
// vends push credentials for" — either way, this never runs a
// customer's build steps on gora8's own infrastructure, only on the
// machine `gora8 deploy` itself is already running on.
func runHostFlow(client *api.Client, agentID, searchPath string) error {
	if !deployHost && deployHostImage == "" {
		return nil
	}

	imageURI := deployHostImage
	if imageURI == "" {
		var err error
		imageURI, err = buildAndPushImage(client, agentID, searchPath)
		if err != nil {
			return fmt.Errorf("build/push image: %w", err)
		}
	}

	spin := ui.NewSpinner("Requesting hosted compute...")
	spin.Start()
	req := &api.HostRequest{
		ImageURI:      imageURI,
		ContainerPort: deployHostPort,
		CPU:           deployHostCPU,
		Memory:        deployHostMemory,
	}
	if _, err := client.HostAgent(agentID, req); err != nil {
		spin.Fail("Hosting request failed")
		return err
	}
	spin.Stop("Provisioning started")

	return pollHosting(client, agentID)
}

// pollHosting waits for POST /host's async provisioning (see
// HostRequest's own doc comment) to leave the "provisioning" state — the
// same reason this polls rather than blocking on a single HTTP call: a
// synchronous wait here would hit this client's own 30s HTTP timeout
// (internal/api/client.go's New()) well before a real provision often
// finishes, the same ALB-idle-timeout-shaped problem the API side fixed
// by making /host itself async in the first place.
func pollHosting(client *api.Client, agentID string) error {
	spin2 := ui.NewSpinner("Waiting for the container to become healthy...")
	spin2.Start()

	const maxWait = 5 * time.Minute
	const pollInterval = 3 * time.Second
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		agent, err := client.GetAgent(agentID)
		if err != nil {
			spin2.Fail("Couldn't check hosting status")
			return err
		}
		switch agent.HostType {
		case "fargate":
			spin2.Stop(fmt.Sprintf("Hosted at %s", agent.Endpoint))
			return nil
		case "host_failed":
			spin2.Fail("Hosting failed")
			return fmt.Errorf("hosting failed: %s", agent.HostError)
		}
		time.Sleep(pollInterval)
	}

	spin2.Fail("Timed out waiting for hosting")
	return fmt.Errorf(
		"still provisioning after %s — check status with `gora8 agents get %s`, it may still succeed",
		maxWait, agentID,
	)
}

// buildAndPushImage requires Docker and (for the vended-credentials
// path) the AWS CLI to already be installed — same "assume the tool is
// there, degrade to a clear error otherwise" convention deploy.go's own
// setupAgentSDK already uses for npm/pip. Neither is bundled with this
// CLI.
func buildAndPushImage(client *api.Client, agentID, searchPath string) (string, error) {
	dockerfilePath := filepath.Join(searchPath, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		return "", fmt.Errorf(
			"no Dockerfile found at %s — add one, or pass --host-image <uri> if you've already built and pushed an image yourself",
			dockerfilePath,
		)
	}

	spinCreds := ui.NewSpinner("Requesting a registry to push to...")
	spinCreds.Start()
	creds, err := client.GetRegistryCredentials(agentID)
	if err != nil {
		spinCreds.Fail("Couldn't get push credentials")
		return "", err
	}
	spinCreds.Stop("Registry ready: " + creds.RepositoryURI)

	tag := creds.RepositoryURI + ":latest"

	spinBuild := ui.NewSpinner("Building image (linux/arm64)...")
	spinBuild.Start()
	// arm64, not the build machine's own architecture — every hosted
	// agent runs on Graviton/ARM64 Fargate (see fargate-hosting.ts's own
	// doc comment: an x86 image crash-loops immediately with "exec
	// format error", never even reaching a health check).
	if out, err := runCombined(searchPath, "docker", "build", "--platform", "linux/arm64", "-t", tag, searchPath); err != nil {
		spinBuild.Fail("docker build failed")
		if out != "" {
			ui.Info(out)
		}
		return "", err
	}
	spinBuild.Stop("Image built")

	spinLogin := ui.NewSpinner("Logging in to registry...")
	spinLogin.Start()
	registryHost := creds.RepositoryURI
	if idx := strings.Index(registryHost, "/"); idx >= 0 {
		registryHost = registryHost[:idx]
	}
	loginCmd := exec.Command("aws", "ecr", "get-login-password", "--region", creds.Region)
	loginCmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+creds.AccessKeyID,
		"AWS_SECRET_ACCESS_KEY="+creds.SecretAccessKey,
		"AWS_SESSION_TOKEN="+creds.SessionToken,
	)
	password, err := loginCmd.Output()
	if err != nil {
		spinLogin.Fail("Couldn't fetch a registry login token")
		return "", fmt.Errorf(
			"aws ecr get-login-password failed: %w — the AWS CLI needs to be installed for the --host push-credential flow; "+
				"alternatively, push the image yourself and pass --host-image <uri>", err,
		)
	}
	dockerLogin := exec.Command("docker", "login", "--username", "AWS", "--password-stdin", registryHost)
	dockerLogin.Stdin = bytes.NewReader(password)
	if out, err := dockerLogin.CombinedOutput(); err != nil {
		spinLogin.Fail("docker login failed")
		if len(out) > 0 {
			ui.Info(string(out))
		}
		return "", err
	}
	spinLogin.Stop("Logged in")

	spinPush := ui.NewSpinner("Pushing image...")
	spinPush.Start()
	if out, err := runCombined(searchPath, "docker", "push", tag); err != nil {
		spinPush.Fail("docker push failed")
		if out != "" {
			ui.Info(out)
		}
		return "", err
	}
	spinPush.Stop("Image pushed")

	return tag, nil
}

// runCombined runs name with args from dir, returning combined
// stdout+stderr alongside any error — same pattern deploy.go's own
// setupAgentSDK helper already uses, so a docker failure shows *why*
// rather than just that it failed.
func runCombined(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func init() {
	deployCmd.Flags().BoolVar(&deployHost, "host", false, "Have gora8 build (from a local Dockerfile) and host this agent's compute on its own Fargate infrastructure — Pro/Enterprise only")
	deployCmd.Flags().StringVar(&deployHostImage, "host-image", "", "Have gora8 host this agent using an image you've already built and pushed yourself, instead of building one — implies --host")
	deployCmd.Flags().IntVar(&deployHostPort, "host-port", 8080, "Port your container listens on, for --host/--host-image")
	deployCmd.Flags().IntVar(&deployHostCPU, "host-cpu", 0, "Fargate CPU units (256=0.25 vCPU, 512, 1024, 2048, 4096) — defaults to 256 if unset")
	deployCmd.Flags().IntVar(&deployHostMemory, "host-memory", 0, "Fargate memory in MiB — defaults to 512 if unset; must be a valid pairing for --host-cpu")
}
