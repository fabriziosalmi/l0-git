package main

import (
	"context"
	"strings"
	"testing"
)

// ── Tier 1: placeholder syntax ────────────────────────────────────────────────

func TestKnownNonSecret_PlaceholderTemplates(t *testing.T) {
	cases := []string{
		"{{my_secret}}",
		"{{ secret_key }}",
		"${MY_SECRET}",
		"${env:SECRET}",
		"$(MY_SECRET)",
		"<MY_TOKEN>",
		"<REPLACE ME>",
		"%MY_SECRET%",
		"[MY_TOKEN_HERE]",
		"__MY_SECRET__",
		"@MY_SECRET@",
		"#{my_secret}",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("template syntax %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_PlaceholderInstructionWords(t *testing.T) {
	cases := []string{
		"changeme",
		"change_me",
		"change-me",
		"CHANGEME",
		"replaceme",
		"replace_me",
		"fill_me_in",
		"todo",
		"not_a_secret",
		"not-a-real-token",
		"dummy",
		"fake",
		"mock",
		"placeholder",
		"redacted",
		"censored",
		"sanitized",
		"masked",
		"test_only",
		"not_set",
		"notset",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("placeholder word %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_PlaceholderRepetitiveChars(t *testing.T) {
	cases := []string{
		"xxxxxxxxxxxxxxxx",
		"XXXXXXXXXXXXXXXX",
		"aaaaaaaaaaaaaaaa",
		"00000000000000000000",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("repetitive-char value %q should be non-secret", v)
		}
	}
}

// ── Tier 2: known service defaults ───────────────────────────────────────────

func TestKnownNonSecret_GenericDefaults(t *testing.T) {
	cases := []string{
		"password", "PASSWORD", "Password",
		"secret", "SECRET",
		"admin", "ADMIN",
		"root", "ROOT",
		"12345678",
		"qwerty",
		"changeme",
		"guest", // RabbitMQ
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("generic default %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_DatabaseDefaults(t *testing.T) {
	cases := []string{
		"postgres", "postgresql", "pgpassword",
		"mysql", "mariadb", "mysqlpassword",
		"mongo", "mongodb", "mongopassword",
		"redis", "redispassword",
		"cassandra",
		"neo4j",
		"influxdb",
		"couchdb",
		"elastic", "elasticsearch",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("database default %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_ServiceDefaults(t *testing.T) {
	cases := []string{
		"minioadmin",   // MinIO official default
		"grafana",      // Grafana default password
		"keycloak",     // Keycloak default
		"sonarqube",    // SonarQube default
		"harbor12345",  // Harbor shipped default
		"5iveL!fe",     // Old GitLab shipped default
		"dev-root-token", // Vault dev-server
		"00000000-0000-0000-0000-000000000000", // null UUID
		"localstack",   // LocalStack default
		"airflow",      // Airflow default
		"rabbitmq",     // RabbitMQ
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("service default %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_JWTTutorialSecrets(t *testing.T) {
	cases := []string{
		"your-256-bit-secret",
		"your-384-bit-secret",
		"your-512-bit-secret",
		"jwtsecret",
		"jwt_secret",
		"this_is_my_secret_key",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("JWT tutorial secret %q should be non-secret", v)
		}
	}
}

// ── Tier 3: test/sandbox key prefixes ────────────────────────────────────────

func TestKnownNonSecret_StripeTestKeys(t *testing.T) {
	// Use structurally valid prefixes but intentionally short/broken suffixes
	// so GitHub push protection does not flag this source file.
	cases := []string{
		"sk_test_EXAMPLE",
		"pk_test_EXAMPLE",
		"rk_test_EXAMPLE",
		"whsec_test_EXAMPLE",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("Stripe test key prefix %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_SquareSandboxKeys(t *testing.T) {
	cases := []string{
		"sandbox-sq0isp-abcdefghijklmnopqrstuvwx",
		"sandbox-sq0atb-abcdefghijklmnopqrstuvwx",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("Square sandbox key %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_CheckoutTestKeys(t *testing.T) {
	cases := []string{
		"test_sk_abcdefghijklmnopqrstuvwxyz",
		"test_pk_abcdefghijklmnopqrstuvwxyz",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("Checkout.com test key %q should be non-secret", v)
		}
	}
}

// ── Tier 4: canonical documentation examples ─────────────────────────────────

func TestKnownNonSecret_AWSDocExamples(t *testing.T) {
	cases := []string{
		"AKIAIOSFODNN7EXAMPLE",
		"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"AKIAI44QH8DHBEXAMPLE",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("AWS doc example %q should be non-secret", v)
		}
	}
}

func TestKnownNonSecret_JWTDocExample(t *testing.T) {
	// The jwt.io debugger pre-fills this token on every page load.
	jwtIO := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	if !isKnownNonSecret(jwtIO) {
		t.Errorf("jwt.io canonical example should be non-secret")
	}
}

func TestKnownNonSecret_AzuriteKey(t *testing.T) {
	key := "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	if !isKnownNonSecret(key) {
		t.Errorf("Azurite well-known storage key should be non-secret")
	}
}

// Slack tokens (xoxb-, xoxp-, …) are REAL OAuth tokens — there is no
// official Slack test/sandbox prefix. The all-same-char check in Tier 1
// catches format examples like xoxb-000000000000-000000000000-XXXXXXXX
// (all-X final segment). Real tokens must NOT be exempted.
func TestKnownNonSecret_SlackRealTokenMustNotBeExempted(t *testing.T) {
	real := "xoxb-1a2B3c4D5e6F7g8H12" // realistic Slack bot token
	if isKnownNonSecret(real) {
		t.Errorf("real-looking Slack token %q must NOT be classified as non-secret", real)
	}
}

func TestKnownNonSecret_TwilioTestCredentials(t *testing.T) {
	cases := []string{
		"ACaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"SKaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	for _, v := range cases {
		if !isKnownNonSecret(v) {
			t.Errorf("Twilio test credential %q should be non-secret", v)
		}
	}
}

// ── Boundary: real-looking high-entropy strings must NOT be exempted ──────────

func TestKnownNonSecret_RealLookingValuesMustFire(t *testing.T) {
	cases := []string{
		// High-entropy strings that look like real keys (not in any exemption list)
		"AKIAJ2EXAMPLE3456789",              // AWS key NOT in doc examples
		"ghp_RealLookingTokenABCDEFGHIJ12",  // realistic GitHub PAT
		"AIzaSyRealGoogleKeyNotInExamples1", // Google API key not in doc examples
	}
	for _, v := range cases {
		if isKnownNonSecret(v) {
			t.Errorf("real-looking value %q was incorrectly classified as non-secret", v)
		}
	}
}

// ── Integration: secrets_scan gate rejects doc examples end-to-end ───────────

func TestSecretsScan_AWSDocExampleNotFlagged(t *testing.T) {
	root := initRepoWithFiles(t, map[string]string{
		"README.md": "Example: AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":aws_access_key") {
			t.Errorf("AWS doc example must not be flagged, got: %+v", f)
		}
	}
}

func TestSecretsScan_JWTIOExampleNotFlagged(t *testing.T) {
	jwtIO := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	root := initRepoWithFiles(t, map[string]string{
		"docs/auth.md": "token: " + jwtIO + "\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":jwt") {
			t.Errorf("jwt.io canonical example must not be flagged, got: %+v", f)
		}
	}
}

func TestSecretsScan_StripeTestKeyNotFlagged(t *testing.T) {
	// Use short form to avoid GitHub push protection on this source file.
	// The isKnownNonSecret check is unit-tested separately in TestKnownNonSecret_StripeTestKeys.
	root := initRepoWithFiles(t, map[string]string{
		"config.yaml": "stripe_key: sk_test_EXAMPLE\n",
	})
	fs, err := checkSecretsScan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if strings.HasSuffix(f.FilePath, ":stripe_live") {
			t.Errorf("Stripe test key must not be flagged, got: %+v", f)
		}
	}
}
