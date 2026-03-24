package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"simple-cli/internal/build"
	"simple-cli/internal/config"
	"simple-cli/internal/deploy"
	"simple-cli/internal/keystore"

	"github.com/spf13/cobra"
)

var authEnv string

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage CLI authentication and enrolled keys",
	Long: `Inspect enrolled keypairs and cached session tokens on this machine,
clear stale tokens, or force re-enrollment after a key rotation.`,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show all enrolled keys and cached session tokens",
	RunE:  runAuthStatus,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear the cached session token for an environment",
	Long: `Clears the on-disk JWT cache for the current workspace's tenant::env.
The next deploy or install will re-authenticate automatically.

Example:
  simple auth logout --env dev`,
	RunE: runAuthLogout,
}

var authEnrollCmd = &cobra.Command{
	Use:   "enroll",
	Short: "Force re-enrollment for the current workspace API key",
	Long: `Deletes the local Ed25519 keypair for the API key in the given environment
and re-enrolls with the Identity Service. Use this after a key rotation.

Example:
  simple auth enroll --env dev`,
	RunE: runAuthEnroll,
}

func init() {
	RootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authEnrollCmd)

	authLogoutCmd.Flags().StringVar(&authEnv, "env", "", "target environment (required)")
	_ = authLogoutCmd.MarkFlagRequired("env")

	authEnrollCmd.Flags().StringVar(&authEnv, "env", "", "target environment (required)")
	_ = authEnrollCmd.MarkFlagRequired("env")
}

type keyInfo struct {
	IDSuffix string `json:"id_suffix"`
	Enrolled bool   `json:"enrolled"`
	Dir      string `json:"dir"`
}

type tokenInfo struct {
	TenantEnv string    `json:"tenant_env"`
	ExpiresAt time.Time `json:"expires_at"`
	Valid     bool      `json:"valid"`
}

func runAuthStatus(_ *cobra.Command, _ []string) error {
	keys := []keyInfo{}
	if entries, err := os.ReadDir(keystore.Dir()); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				id := e.Name()
				keys = append(keys, keyInfo{
					IDSuffix: id,
					Enrolled: keystore.IsEnrolled(id),
					Dir:      filepath.Join(keystore.Dir(), id),
				})
			}
		}
	}

	store := &deploy.FileTokenStore{}
	cache, err := store.Load()

	var warningMsg string
	defer func() {
		if warningMsg != "" && !jsonOutput {
			fmt.Fprintf(os.Stderr, "\n⚠️  Warning: Failed to read session cache: %v\n", warningMsg)
		}
	}()

	if err != nil {
		warningMsg = err.Error()
		cache = &deploy.TokenCache{Tokens: make(map[string]deploy.CachedToken)}
	}

	tokens := []tokenInfo{}
	for k, v := range cache.Tokens {
		tokens = append(tokens, tokenInfo{
			TenantEnv: k,
			ExpiresAt: v.ExpiresAt,
			Valid:     time.Now().Before(v.ExpiresAt),
		})
	}

	if jsonOutput {
		out := map[string]interface{}{"keys": keys, "tokens": tokens}
		if warningMsg != "" {
			out["warning"] = warningMsg
		}
		return printJSON(out)
	}

	fmt.Printf("Enrolled Keys (%d):\n", len(keys))
	for _, k := range keys {
		status := "✅ enrolled"
		if !k.Enrolled {
			status = "⚠️  keypair exists, not enrolled"
		}
		fmt.Printf("  %-24s  %s\n", k.IDSuffix, status)
	}

	fmt.Printf("\nCached Session Tokens (%d):\n", len(tokens))
	for _, t := range tokens {
		status := "✅ valid"
		if !t.Valid {
			status = "⛔ expired"
		}
		fmt.Printf("  %-20s  %s  (exp %s)\n", t.TenantEnv, status, t.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}

func runAuthLogout(_ *cobra.Command, _ []string) error {
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return fmt.Errorf("failed to ensure scl-parser: %w", err)
	}

	cfg, err := config.NewLoader(parserPath).LoadSimpleSCL(".")
	if err != nil {
		return fmt.Errorf("failed to load simple.scl (are you in a Simple Platform workspace?): %w", err)
	}

	tenantEnvKey := deploy.TenantEnvKey(cfg.Tenant, authEnv)
	auth := deploy.NewAuthenticator()
	
	if err := auth.ClearCache(tenantEnvKey); err != nil {
		return fmt.Errorf("failed to clear session token: %w", err)
	}

	if jsonOutput {
		return printJSON(map[string]string{"status": "success", "cleared": tenantEnvKey})
	}
	fmt.Printf("✅ Cleared session token for %s\n", tenantEnvKey)
	return nil
}

func runAuthEnroll(cmd *cobra.Command, _ []string) error {
	parserPath, err := build.EnsureSCLParser(nil)
	if err != nil {
		return fmt.Errorf("failed to ensure scl-parser: %w", err)
	}

	cfg, err := config.NewLoader(parserPath).LoadSimpleSCL(".")
	if err != nil {
		return fmt.Errorf("failed to load simple.scl (are you in a Simple Platform workspace?): %w", err)
	}

	env, err := cfg.GetEnv(authEnv)
	if err != nil {
		return err
	}

	rawKey := os.ExpandEnv(env.APIKey)
	idSuffix, err := deploy.ParseIDSuffix(rawKey)
	if err != nil {
		return fmt.Errorf("invalid api key in simple.scl: %w", err)
	}

	// Wipe keypair — next GetJWT call will re-generate and re-enroll.
	if err := keystore.DeleteKey(idSuffix); err != nil {
		return fmt.Errorf("failed to delete keypair for %s: %w", idSuffix, err)
	}

	tenantEnvKey := deploy.TenantEnvKey(cfg.Tenant, authEnv)
	auth := deploy.NewAuthenticator()
	if err := auth.ClearCache(tenantEnvKey); err != nil { // clear stale session JWT too
		fmt.Printf("⚠️  Warning: Failed to clear stale token for %s: %v\n", tenantEnvKey, err)
	}

	if !jsonOutput {
		fmt.Printf("🔑 Re-enrolling %s for %s...\n", idSuffix, tenantEnvKey)
	}

	if _, err := auth.GetJWT(cmd.Context(), env.IdentityEndpoint(), rawKey, tenantEnvKey); err != nil {
		return fmt.Errorf("re-enrollment failed: %w", err)
	}

	if jsonOutput {
		return printJSON(map[string]string{
			"status":    "success",
			"id_suffix": idSuffix,
			"env":       tenantEnvKey,
		})
	}
	fmt.Printf("✅ Re-enrolled %s for %s\n", idSuffix, tenantEnvKey)
	return nil
}
