package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"simple-cli/internal/build"
	"simple-cli/internal/config"
	"simple-cli/internal/deploy"
	"simple-cli/internal/ui"
)

// devopsAuthenticator is the part of deploy.Authenticator that deploy and
// install use.
type devopsAuthenticator interface {
	GetJWT(ctx context.Context, endpoint, apiKey, tenantEnvKey string) (string, error)
	ClearCache(tenantEnvKey string) error
}

// devopsClient is the part of deploy.Client that deploy and install use.
type devopsClient interface {
	JoinChannel(ctx context.Context, appID string) error
	SendManifest(ctx context.Context, files map[string]deploy.FileInfo, version string) ([]string, error)
	SendFiles(ctx context.Context, files map[string]deploy.FileInfo, needed []string, onProgress func(deploy.UploadProgress)) error
	Deploy(ctx context.Context) (*deploy.DeployResult, error)
	Install(ctx context.Context) (*deploy.InstallResult, error)
	Close()
}

// devopsDeps are the side effects deploy and install share: finding the
// scl-parser, reading simple.scl, signing in and dialling the devops server.
// Tests replace them; defaultDevopsDeps wires the real ones.
type devopsDeps struct {
	ensureParser     func(onStatus func(string)) (string, error)
	loadConfig       func(parserPath string) (*config.SimpleSCL, error)
	newAuthenticator func() devopsAuthenticator
	dial             func(ctx context.Context, endpoint, jwt string) (devopsClient, error)
}

func defaultDevopsDeps() devopsDeps {
	return devopsDeps{
		ensureParser: build.EnsureSCLParserFunc,
		loadConfig: func(parserPath string) (*config.SimpleSCL, error) {
			return config.NewLoader(parserPath).LoadSimpleSCL(".")
		},
		newAuthenticator: func() devopsAuthenticator { return deploy.NewAuthenticator() },
		dial:             dialDevops,
	}
}

// dialDevops connects a client to the devops server at endpoint.
//
// Installs are synchronous server-side and routinely exceed 30s on
// record-heavy apps, so every client carries the long reply timeout,
// including the one dialled again after a token refresh.
func dialDevops(ctx context.Context, endpoint, jwt string) (devopsClient, error) {
	client := deploy.NewClient(deploy.ClientConfig{
		Endpoint: endpoint,
		JWT:      jwt,
		Timeout:  deploy.DefaultTimeout,
	})
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// devopsTarget is one environment of the project: where its devops server
// is, and the credentials to reach it.
type devopsTarget struct {
	deps         devopsDeps
	parserPath   string
	envName      string
	tenantEnvKey string
	jwt          string
	cfg          *config.SimpleSCL
	env          *config.Environment
	auth         devopsAuthenticator
}

// loadDevopsTarget finds the scl-parser (downloading it if needed), loads
// simple.scl from the working directory and resolves envName in it. The
// API key is checked only when the target signs in: a deploy dry run loads
// the target and never does. It reports only details and notes on the
// config step: the caller owns the step's start and end.
func loadDevopsTarget(deps devopsDeps, envName string, steps ui.StepReporter) (*devopsTarget, error) {
	var downloading sync.Once
	parserPath, err := deps.ensureParser(func(status string) {
		// The first status means a download started; that is worth keeping
		// in a log, while the percentages that follow are not.
		downloading.Do(func() { steps.Note(stepConfig, "downloading scl-parser") })
		steps.Detail(stepConfig, "scl-parser: "+parserStatus(status))
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ensure scl-parser: %w", err)
	}

	cfg, err := deps.loadConfig(parserPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load simple.scl: %w", err)
	}

	env, err := cfg.LookupEnv(envName)
	if err != nil {
		return nil, err
	}

	return &devopsTarget{
		deps:         deps,
		parserPath:   parserPath,
		envName:      envName,
		tenantEnvKey: deploy.TenantEnvKey(cfg.Tenant, envName),
		cfg:          cfg,
		env:          env,
		auth:         deps.newAuthenticator(),
	}, nil
}

// parserStatus turns the tool installer's "Downloading 43%..." into
// "downloading 43%".
func parserStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(status), "...")))
}

// host is the devops server the target connects to.
func (t *devopsTarget) host() string {
	return t.env.DevOpsEndpoint()
}

// authenticate gets a JWT for the target, from the cache while it is valid.
// It first checks that the environment's API key is set.
func (t *devopsTarget) authenticate(ctx context.Context) error {
	if _, err := t.cfg.GetEnv(t.envName); err != nil {
		return err
	}
	jwt, err := t.auth.GetJWT(ctx, t.env.IdentityEndpoint(), t.env.APIKey, t.tenantEnvKey)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}
	t.jwt = jwt
	return nil
}

// connect dials the devops server and joins appID's deploy channel. A server
// that rejects the cached token (401/403) gets one fresh sign-in and one more
// dial: tokens can be revoked or rotated server-side before they expire. A
// failed join closes the client, so the caller only closes what it gets.
func (t *devopsTarget) connect(ctx context.Context, appID string, steps ui.StepReporter) (devopsClient, error) {
	client, err := t.deps.dial(ctx, t.host(), t.jwt)
	var authErr *deploy.AuthFailedError
	switch {
	case errors.As(err, &authErr):
		steps.Note(stepConnect, "server rejected the saved session; signing in again")
		if client, err = t.reconnect(ctx); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	}

	if err := client.JoinChannel(ctx, appID); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// reconnect clears the cached token, signs in again and dials once more.
func (t *devopsTarget) reconnect(ctx context.Context) (devopsClient, error) {
	if err := t.auth.ClearCache(t.tenantEnvKey); err != nil {
		return nil, fmt.Errorf("failed to clear token cache: %w", err)
	}
	jwt, err := t.auth.GetJWT(ctx, t.env.IdentityEndpoint(), t.env.APIKey, t.tenantEnvKey)
	if err != nil {
		return nil, fmt.Errorf("re-authentication failed: %w", err)
	}
	t.jwt = jwt

	client, err := t.deps.dial(ctx, t.host(), jwt)
	if err != nil {
		return nil, fmt.Errorf("connection failed after token refresh: %w", err)
	}
	return client, nil
}
