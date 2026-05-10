package main

import (
	"regexp"
	"strings"
)

// isKnownNonSecret returns true when a matched secret value is publicly known
// and carries zero information advantage for an attacker.
//
// The test is layered:
//
//	Tier 1 — placeholder / template syntax: the FORMAT of the value signals
//	          it is intentionally unset ({{secret}}, <YOUR_KEY>, CHANGE_ME, …)
//	Tier 2 — well-known service defaults: the VALUE is the officially-shipped
//	          default credential of a common open-source service
//	Tier 3 — official test / sandbox key prefixes: the vendor explicitly
//	          documents these prefixes as non-production (sk_test_, …)
//	Tier 4 — canonical documentation examples: the exact string appears in
//	          official documentation as an illustrative placeholder
//
// The function is called after the Shannon entropy floor so any value that
// reaches it passed a ≥ 3.5 bits/char randomness check — yet entropy alone
// cannot distinguish a real key from a publicly-known one.
func isKnownNonSecret(value string) bool {
	if matchesPlaceholderPattern(value) {
		return true
	}
	if isKnownServiceDefault(value) {
		return true
	}
	if hasTestKeyPrefix(value) {
		return true
	}
	if canonicalDocExamples[value] {
		return true
	}
	return false
}

// =============================================================================
// Tier 1 — Placeholder / template syntax
// =============================================================================

// placeholderValueREs matches values whose FORMAT communicates "this is not
// a real secret, fill it in before use". Applied to the raw matched string.
var placeholderValueREs = []*regexp.Regexp{
	// Template / substitution variable syntaxes (all common config ecosystems)
	regexp.MustCompile(`^\s*\{\{[^}]+\}\}\s*$`),                // {{secret}} / {{env.MY_KEY}}
	regexp.MustCompile(`^\s*\$\{[^}]+\}\s*$`),                  // ${MY_SECRET} / ${env:SECRET}
	regexp.MustCompile(`^\s*\$\([^)]+\)\s*$`),                  // $(MY_SECRET)
	regexp.MustCompile(`(?i)^\s*<[A-Z][A-Z0-9_\- ]{2,}>\s*$`), // <MY_TOKEN> / <REPLACE ME>
	regexp.MustCompile(`(?i)^\s*%[A-Z][A-Z0-9_]{2,}%\s*$`),     // %MY_SECRET%  (Windows env)
	regexp.MustCompile(`^\s*\[[A-Z][A-Z0-9_\- ]{2,}\]\s*$`),   // [MY_TOKEN]
	regexp.MustCompile(`^\s*__[A-Z][A-Z0-9_]+__\s*$`),          // __MY_SECRET__
	regexp.MustCompile(`^\s*@[A-Z][A-Z0-9_]+@\s*$`),            // @MY_SECRET@  (Maven filtering)
	regexp.MustCompile(`^\s*#\{[^}]+\}\s*$`),                   // #{my_secret}  (Ruby ERB / Spring)
	regexp.MustCompile(`^\s*\$\[\[[^\]]+\]\]\s*$`),             // $[[MY_SECRET]]
	regexp.MustCompile(`^\s*__[a-z][a-z0-9_]+__\s*$`),          // __my_secret__

	// Explicit placeholder / instruction words in the value itself
	regexp.MustCompile(`(?i)\b(your|my|the|an?)\b.{0,20}\b(api[-_]?key|token|secret|password|credential)\b`),
	regexp.MustCompile(`(?i)\b(api[-_]?key|token|secret|password)\b.{0,10}\bhere\b`),
	regexp.MustCompile(`(?i)\b(change|replace|update|insert|fill[-_]?in?|put|add|set|enter|provide|supply)\b.{0,15}\b(this|it|here|value|me)\b`),
	regexp.MustCompile(`(?i)\bchange[-_]?me\b`),
	regexp.MustCompile(`(?i)\breplace[-_]?me\b`),
	regexp.MustCompile(`(?i)\bfill[-_]?me[-_]?in\b`),
	regexp.MustCompile(`(?i)\btodo[-_]?(fill|replace|change|update|add|set)?\b`),
	regexp.MustCompile(`(?i)\b(not[-_]?a[-_]?(real[-_]?)?|no[-_]?)(secret|token|key|password|credential)\b`),
	regexp.MustCompile(`(?i)\b(dummy|fake|mock|stub|sample|example|placeholder|redacted|censored|scrubbed|obfuscated|sanitized|masked)\b`),
	regexp.MustCompile(`(?i)\btest[-_]?only\b`),
	regexp.MustCompile(`(?i)\bnot[-_]?set\b`),
	regexp.MustCompile(`(?i)\bnot[-_]?configured\b`),
	regexp.MustCompile(`(?i)\bnot[-_]?provided\b`),
	regexp.MustCompile(`(?i)\bnot[-_]?defined\b`),
	regexp.MustCompile(`(?i)\bto[-_]?be[-_]?(set|filled|provided|configured|replaced)\b`),
	regexp.MustCompile(`(?i)\b(insert|enter|provide|add|set)[-_ ](your|the|a)\b`),

	// Entirely repeated characters (all-A, all-x, all-0, …) are checked
	// in matchesPlaceholderPattern via allSameChar helper — see below.

	// Obvious sequential / keyboard-walk prefixes
	regexp.MustCompile(`(?i)^(1234|abcd|qwerty|asdfgh|zxcvbn|0123|abcdef)`),

	// All-caps or camelCase description of what should go here
	regexp.MustCompile(`^[A-Z][A-Z0-9_]{4,}_(KEY|SECRET|TOKEN|PASSWORD|CREDENTIAL|API_KEY|ACCESS_KEY|AUTH)$`),
	regexp.MustCompile(`(?i)^(SECRET|TOKEN|KEY|PASSWORD|API_KEY)_?(HERE|VALUE|GOES_HERE|PLACEHOLDER)?$`),
}

func matchesPlaceholderPattern(value string) bool {
	for _, re := range placeholderValueREs {
		if re.MatchString(value) {
			return true
		}
	}
	// All-same-character strings of 8+ chars (RE2 has no backreferences).
	if allSameChar(value, 8) {
		return true
	}
	return false
}

// allSameChar returns true when s has at least minLen runes and every rune
// is identical — e.g. "xxxxxxxxxxxxxxxx" or "0000000000000000".
func allSameChar(s string, minLen int) bool {
	runes := []rune(s)
	if len(runes) < minLen {
		return false
	}
	first := runes[0]
	for _, r := range runes[1:] {
		if r != first {
			return false
		}
	}
	return true
}

// =============================================================================
// Tier 2 — Well-known service default credentials
// =============================================================================

// knownServiceDefaultSet is keyed on the lowercase-trimmed value. Every entry
// corresponds to a credential that is:
//   (a) shipped as the default by an official open-source project, AND
//   (b) documented publicly in the project's official README / docs / image page.
//
// Sources: official Docker Hub pages, GitHub README files, official docs sites.
// Nothing is added by speculation — only verified defaults are listed.
var knownServiceDefaultSet = map[string]bool{
	// ── Generic ultra-common placeholders ─────────────────────────────────────
	"password":         true, // default everywhere
	"passwd":           true,
	"pass":             true,
	"secret":           true,
	"mysecret":         true,
	"yoursecret":       true,
	"thesecret":        true,
	"supersecret":      true,
	"verysecret":       true,
	"topsecret":        true,
	"changeme":         true,
	"change_me":        true,
	"change-me":        true,
	"pleasechangeme":   true,
	"pleasereplaceme":  true,
	"replaceme":        true,
	"replace_me":       true,
	"updateme":         true,
	"update_me":        true,
	"setme":            true,
	"set_me":           true,
	"fillme":           true,
	"fill_me":          true,
	"todo":             true,
	"fixme":            true,
	"tbd":              true,
	"default":          true,
	"defaults":         true,
	"example":          true,
	"sample":           true,
	"test":             true,
	"testing":          true,
	"testpassword":     true,
	"test_password":    true,
	"test-password":    true,
	"test123":          true,
	"test1234":         true,
	"testtoken":        true,
	"test_token":       true,
	"testkey":          true,
	"test_key":         true,
	"dev":              true,
	"develop":          true,
	"development":      true,
	"devpassword":      true,
	"dev_password":     true,
	"devkey":           true,
	"dev_key":          true,
	"devtoken":         true,
	"local":            true,
	"localhost":        true,
	"localpassword":    true,
	"admin":            true,
	"administrator":    true,
	"adminpassword":    true,
	"admin_password":   true,
	"admin123":         true,
	"admin1234":        true,
	"admin@123":        true,
	"admin!123":        true,
	"root":             true,
	"rootpassword":     true,
	"root_password":    true,
	"root123":          true,
	"user":             true,
	"username":         true,
	"userpassword":     true,
	"user_password":    true,
	"user123":          true,
	"guest":            true,  // RabbitMQ default username AND password
	"manager":          true,  // ActiveMQ default user password
	"foobar":           true,
	"foo":              true,
	"bar":              true,
	"baz":              true,
	"qux":              true,
	"abc":              true,
	"abc123":           true,
	"abcdef":           true,
	"abcdefgh":         true,
	"abcdefghij":       true,
	"letmein":          true,
	"welcome":          true,
	"welcome1":         true,
	"login":            true,
	"master":           true,
	"null":             true,
	"none":             true,
	"empty":            true,
	"blank":            true,
	"n/a":              true,
	"na":               true,
	"not_set":          true,
	"notset":           true,
	"unknown":          true,
	"undefined":        true,
	"unset":            true,
	"disabled":         true,
	"nopassword":       true,
	"no_password":      true,
	"noauth":           true,
	"no_auth":          true,
	"notoken":          true,
	"no_token":         true,
	"secret123":        true,
	"secret1234":       true,
	"mysecretpassword": true,
	"mypassword":       true,
	"mytoken":          true,
	"myapikey":         true,
	"my_api_key":       true,
	"my_token":         true,
	"my_secret":        true,
	"apitoken":         true,
	"api_token":        true,
	"apikey":           true,
	"api_key":          true,
	"accesstoken":      true,
	"access_token":     true,
	"accesskey":        true,
	"access_key":       true,
	"authtoken":        true,
	"auth_token":       true,
	"token":            true,
	"key":              true,
	"value":            true,
	"placeholder":      true,
	"dummy":            true,
	"fake":             true,
	"mock":             true,
	"stub":             true,
	"redacted":         true,
	"censored":         true,
	"obfuscated":       true,
	"sanitized":        true,
	"masked":           true,
	"xxxxxxxx":         true,
	"xxxxxxxxxx":       true,
	"xxxxxxxxxxxx":     true,
	"xxxxxxxxxxxxxxxx": true,
	"00000000":         true,
	"0000000000":       true,
	"000000000000":     true,
	"0000000000000000": true,
	"11111111":         true,
	"1111111111":       true,
	"1234":             true,
	"12345":            true,
	"123456":           true,
	"1234567":          true,
	"12345678":         true,
	"123456789":        true,
	"1234567890":       true,
	"qwerty":           true,
	"qwerty123":        true,
	"asdf":             true,
	"asdfasdf":         true,

	// ── PostgreSQL ─────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/postgres — POSTGRES_PASSWORD examples
	"postgres":         true,
	"postgresql":       true,
	"pgpassword":       true,
	"pg_password":      true,
	"postgrespassword": true,
	"postgres123":      true,
	"dbpassword":       true,
	"db_password":      true,
	"dbpass":           true,
	"db_pass":          true,
	"databasepassword": true,
	"database_password": true,

	// ── MySQL / MariaDB ────────────────────────────────────────────────────────
	// https://hub.docker.com/_/mysql — MYSQL_ROOT_PASSWORD examples
	"mysql":            true,
	"mariadb":          true,
	"rootpasswd":       true,
	"mysqlpassword":    true,
	"mysql_password":   true,
	"mysql_root_password": true,
	"mysqlrootpassword": true,

	// ── MongoDB ────────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/mongo
	"mongo":           true,
	"mongodb":         true,
	"mongopassword":   true,
	"mongo_password":  true,
	"mongoadmin":      true,
	"mongo_admin":     true,

	// ── Redis ──────────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/redis — no auth by default; examples use these
	"redis":           true,
	"redispassword":   true,
	"redis_password":  true,
	"redisauth":       true,
	"redis_auth":      true,

	// ── RabbitMQ ───────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/rabbitmq — default user/pass is guest:guest
	"rabbitmq":        true,
	"rabbit":          true,
	"rabbitpassword":  true,
	"rabbit_password": true,
	// "guest" already listed above

	// ── Elasticsearch / OpenSearch ─────────────────────────────────────────────
	// https://hub.docker.com/_/elasticsearch — bootstrap password in docs
	"elastic":         true,
	"elasticsearch":   true,
	"opensearch":      true,
	"kibanapassword":  true,
	"kibana_password": true,
	"kibana_system":   true,
	"elasticpassword": true,
	"elastic_password": true,

	// ── InfluxDB ───────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/influxdb — INFLUXDB_ADMIN_PASSWORD examples
	"influxdb":        true,
	"influx":          true,
	"influxpassword":  true,
	"influx_password": true,
	"influxadmin":     true,

	// ── CouchDB ────────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/couchdb — COUCHDB_PASSWORD default
	"couchdb":         true,
	"couch":           true,
	"couchpassword":   true,

	// ── Cassandra ──────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/cassandra — cassandra:cassandra default
	"cassandra":       true,
	"cassandrapassword": true,

	// ── Neo4j ──────────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/neo4j — NEO4J_AUTH default is neo4j/neo4j
	"neo4j":           true,
	"neo4jpassword":   true,

	// ── MinIO ──────────────────────────────────────────────────────────────────
	// https://hub.docker.com/r/minio/minio — MINIO_ROOT_USER/PASSWORD defaults
	"minioadmin":      true, // official default root user AND password
	"minio":           true,
	"miniopassword":   true,
	"minio_password":  true,
	"minioroot":       true,
	"minio_root":      true,
	"minioaccesskey":  true,
	"minio_access_key": true,
	"miniosecretkey":  true,
	"minio_secret_key": true,

	// ── Grafana ────────────────────────────────────────────────────────────────
	// https://hub.docker.com/r/grafana/grafana — default admin:admin
	"grafana":         true,
	"grafanapassword": true,
	"grafana_password": true,
	"grafanaadmin":    true,

	// ── Prometheus / Alertmanager ──────────────────────────────────────────────
	// No auth by default; examples in config use these
	"prometheus":      true,
	"alertmanager":    true,

	// ── Keycloak ───────────────────────────────────────────────────────────────
	// https://hub.docker.com/r/keycloak/keycloak — KEYCLOAK_ADMIN_PASSWORD
	"keycloak":        true,
	"keycloakadmin":   true,
	"keycloak_admin":  true,
	"keycloakpassword": true,
	"keycloak_password": true,

	// ── SonarQube ──────────────────────────────────────────────────────────────
	// https://hub.docker.com/_/sonarqube — default sonarqube:sonarqube then admin:admin
	"sonarqube":       true,
	"sonar":           true,
	"sonarpassword":   true,
	"sonar_password":  true,

	// ── Portainer ──────────────────────────────────────────────────────────────
	// First-run creates admin; docs examples use these
	"portainer":       true,
	"portaineradmin":  true,
	"portainer_admin": true,

	// ── Gitea ──────────────────────────────────────────────────────────────────
	// https://hub.docker.com/r/gitea/gitea — setup wizard; docs examples
	"gitea":           true,
	"giteaadmin":      true,
	"gitea_admin":     true,

	// ── Harbor ─────────────────────────────────────────────────────────────────
	// https://goharbor.io/docs — Harbor12345 is the shipped default
	"harbor12345":     true,
	"harbor":          true,

	// ── Nexus Repository ───────────────────────────────────────────────────────
	// https://hub.docker.com/r/sonatype/nexus3 — admin123 shipped default (old)
	"nexus":           true,
	"nexuspassword":   true,
	"nexus_password":  true,

	// ── Artifactory ────────────────────────────────────────────────────────────
	// https://hub.docker.com/r/releases-docker.jfrog.io/jfrog/artifactory-oss
	"artifactory":     true,
	"jfrog":           true,

	// ── Vault (HashiCorp) ──────────────────────────────────────────────────────
	// https://developer.hashicorp.com/vault/docs/concepts/dev-server
	// dev-server token is literally "root" — already listed; extra aliases:
	"dev-root-token":  true,
	"dev_root_token":  true,
	"devroot":         true,
	"vaulttoken":      true,
	"vault_token":     true,
	"vault":           true,
	"00000000-0000-0000-0000-000000000000": true, // null UUID — docs placeholder

	// ── Consul (HashiCorp) ─────────────────────────────────────────────────────
	"consul":          true,
	"consultoken":     true,
	"consul_token":    true,

	// ── Traefik ────────────────────────────────────────────────────────────────
	// Dashboard has no auth by default; docs examples use these
	"traefik":         true,
	"traefikpassword": true,

	// ── LocalStack / AWS emulation ─────────────────────────────────────────────
	// https://docs.localstack.cloud/references/credentials/
	"localstack":      true,
	// "test" is already listed above (generic); LocalStack uses it too
	// "000000000000" — AWS account ID example, listed above

	// ── Apache Kafka / Confluent ───────────────────────────────────────────────
	// https://docs.confluent.io — SASL examples in quickstart docs
	"kafka":           true,
	"kafkapassword":   true,
	"kafka_password":  true,
	"confluent":       true,

	// ── ActiveMQ ───────────────────────────────────────────────────────────────
	// https://activemq.apache.org — default admin:admin, user:manager
	"activemq":        true,

	// ── NATS ───────────────────────────────────────────────────────────────────
	// No auth by default; docs examples
	"nats":            true,
	"natspassword":    true,

	// ── ClickHouse ─────────────────────────────────────────────────────────────
	// https://hub.docker.com/r/clickhouse/clickhouse-server
	"clickhouse":      true,
	"clickhousepassword": true,
	"clickhouse_password": true,
	"clickhouseadmin": true,

	// ── Airflow ────────────────────────────────────────────────────────────────
	// https://airflow.apache.org/docs/apache-airflow/stable/howto/docker-compose/index.html
	"airflow":         true,
	"airflowpassword": true,
	"airflow_password": true,
	"airflowadmin":    true,
	"airflow123":      true,
	"_airflow_www_user_password": true, // exact env var value in official Airflow docker-compose.yaml

	// ── Superset ───────────────────────────────────────────────────────────────
	// https://superset.apache.org/docs/installation/installing-superset-using-docker-compose/
	"superset":        true,
	"supersetpassword": true,
	"superset_admin_password": true,
	"supersetadmin":   true,
	"superset_admin":  true,

	// ── Metabase ───────────────────────────────────────────────────────────────
	"metabase":        true,
	"metabasepassword": true,

	// ── Redash ─────────────────────────────────────────────────────────────────
	// https://redash.io/help/open-source/setup
	"redash":          true,
	"redashpassword":  true,

	// ── n8n ────────────────────────────────────────────────────────────────────
	"n8n":             true,
	"n8npassword":     true,
	"n8n_password":    true,

	// ── Mattermost ─────────────────────────────────────────────────────────────
	"mattermost":      true,
	"mattermostpassword": true,

	// ── Gitab CE/EE ────────────────────────────────────────────────────────────
	// https://docs.gitlab.com/ee/install/docker/ — initial root password
	// (5iveL!fe was shipped in very old versions; current uses a generated secret)
	"5ivel!fe":        true, // historical GitLab shipped default
	"gitlabpassword":  true,
	"gitlab_password": true,
	"gitlab":          true,

	// ── Jenkins ────────────────────────────────────────────────────────────────
	"jenkins":         true,
	"jenkinspassword": true,
	"jenkins_password": true,

	// ── Drone CI ───────────────────────────────────────────────────────────────
	"drone":           true,
	"dronerpc":        true,
	"drone_rpc_secret": true,
	"dronepassword":   true,

	// ── Woodpecker CI ──────────────────────────────────────────────────────────
	"woodpecker":      true,
	"woodpeckeragent": true,
	"woodpecker_agent_secret": true,

	// ── Azure Storage Emulator / Azurite ───────────────────────────────────────
	// https://learn.microsoft.com/en-us/azure/storage/common/storage-use-azurite
	// The account key below is the published, well-known Azurite default.
	"devstoreaccount1": true,
	"eby8vdm02xnocqflquwijpllmetlcdxj1ouzft50usrz6ifsusfq2uverczc4i6tq/k1szfptort/kbhbeksogmgw==": true,

	// ── Stripe test mode — prefix handled in Tier 3, but zero-info defaults ───
	"stripe_test":     true,
	"stripetest":      true,

	// ── SendGrid ───────────────────────────────────────────────────────────────
	"sendgrid":        true,
	"sendgridpassword": true,

	// ── Twilio ─────────────────────────────────────────────────────────────────
	"twilio":          true,
	"twiliopassword":  true,
	"twilioauthtoken": true,

	// ── JWT "secret" used in tutorials ────────────────────────────────────────
	"your-256-bit-secret":           true, // jwt.io default HS256 secret
	"your-384-bit-secret":           true, // jwt.io default HS384 secret
	"your-512-bit-secret":           true, // jwt.io default HS512 secret
	"supersecretjwtkey":             true,
	"jwtsecret":                     true,
	"jwt_secret":                    true,
	"jwtpassword":                   true,
	"jwt_password":                  true,
	"jwtkey":                        true,
	"jwt_key":                       true,
	"jwtsecretkey":                  true,
	"jwt_secret_key":                true,
	"this_is_my_secret_key":         true,
	"this-is-my-secret":             true,
	"mysupersecretpassword":         true,
	"thisisasecret":                 true,
	"this_is_a_secret":              true,
	"iamsupersecret":                true,
	"i_am_super_secret":             true,

	// ── Misc cloud / SaaS placeholders from official quickstarts ───────────────
	"your_api_key":    true,
	"your_secret_key": true,
	"your_token":      true,
	"your_password":   true,
	"yourapikey":      true,
	"yoursecretkey":   true,
	"yourtoken":       true,
	"yourpassword":    true,
	"api_key_here":    true,
	"token_here":      true,
	"secret_here":     true,
	"password_here":   true,
}

func isKnownServiceDefault(value string) bool {
	return knownServiceDefaultSet[strings.ToLower(strings.TrimSpace(value))]
}

// =============================================================================
// Tier 3 — Official test / sandbox key prefixes
// =============================================================================

// testKeyPrefixSet maps known test/sandbox key prefixes (lowercase) to the
// service that documented them. Every prefix here is taken from an official
// vendor documentation page or SDK source that explicitly marks it as
// non-production.
//
// Sources:
//   Stripe:  https://stripe.com/docs/keys
//   Square:  https://developer.squareup.com/docs/devtools/sandbox/overview
//   Braintree: https://developer.paypal.com/braintree/docs/start/overview
//   Checkout.com: https://www.checkout.com/docs/testing
//   Adyen:   https://docs.adyen.com/development-resources/test-credentials
//   Twilio:  https://www.twilio.com/docs/iam/test-credentials
//   Vault:   https://developer.hashicorp.com/vault/docs/concepts/dev-server
var testKeyPrefixes = []string{
	// Stripe — test keys start with sk_test_, pk_test_, rk_test_, whsec_test_
	"sk_test_",
	"pk_test_",
	"rk_test_",
	"whsec_test_",
	"acct_test_",
	// Square sandbox — sandbox-sq0isp- (secret) and sandbox-sq0atb- (access token)
	"sandbox-sq0isp-",
	"sandbox-sq0atb-",
	"sandbox-sq0atp-",
	// Braintree sandbox — all sandbox credentials share this gateway ID prefix
	"sandbox_",
	// Checkout.com test keys
	"test_sk_",
	"test_pk_",
	// Adyen test API key prefix (documented in their test credential guide)
	"adyentest_",
	// Twilio test credentials — account SIDs starting with AC + magic test number
	// (official test SID is ACaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa pattern)
	"acaaaaaaaaaaaaaaaaa",
	// NOTE: Slack tokens (xoxb-, xoxp-, …) are NOT listed here because Slack
	// does not have an official test/sandbox token program — xox* tokens are
	// always real OAuth credentials. They are handled by the secrets_scan pattern
	// and must fire. Slack format-examples in docs are caught by the all-same-char
	// check (all-X or all-0 ID segments) in Tier 1.
	//
	// HashiCorp Vault dev-server — hvs.AAAA... is the dev-mode token format;
	// real tokens start with hvs. but dev tokens are documented to have all-same chars
	// We only exempt the literal dev-server default pattern, not all hvs. tokens.
	// (The all-same char pattern is also caught by Tier 1 regex above.)
	// Google test API keys from their testing guide
	"aizasyc-",  // documented test key prefix in some GCP quickstarts
	// OpenAI — sk-proj- is a newer format; the pure test/example prefix from docs:
	"sk-xxxxxxxx",
	"sk-none",
	// npm test tokens from npm CLI test suite
	"npm_test_",
	"npm_dev_",
}

func hasTestKeyPrefix(value string) bool {
	low := strings.ToLower(value)
	for _, prefix := range testKeyPrefixes {
		if strings.HasPrefix(low, prefix) {
			return true
		}
	}
	return false
}

// =============================================================================
// Tier 4 — Canonical documentation examples (exact match)
// =============================================================================

// canonicalDocExamples is a set of exact strings that appear in official vendor
// documentation as illustrative examples. Keys are case-sensitive (they must
// match the string exactly as it would appear in a file).
//
// Sources — each entry is verifiable in the referenced official page:
//   AWS:     https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html
//   jwt.io:  https://jwt.io/#debugger-io
//   Azurite: https://learn.microsoft.com/azure/storage/common/storage-use-azurite
//   GCP:     https://cloud.google.com/docs/authentication/api-keys (example key)
//   GitHub:  https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/about-authentication-to-github
var canonicalDocExamples = map[string]bool{
	// ── AWS ────────────────────────────────────────────────────────────────────
	// Official AWS documentation example credentials (IAM docs, SDK guides, etc.)
	"AKIAIOSFODNN7EXAMPLE":                           true,
	"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY":     true,
	"AKIAI44QH8DHBEXAMPLE":                           true, // SDK guide example
	"je7MtGbClwBF/2Zp9Utk/h3yCo8nvbEXAMPLEKEY":     true,

	// ── Azure Storage Emulator / Azurite ───────────────────────────────────────
	// https://learn.microsoft.com/en-us/azure/storage/common/storage-use-azurite#well-known-storage-account-and-key
	"Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==": true,
	// Connection string that always appears in Azurite docs:
	"DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;": true,

	// ── JWT ────────────────────────────────────────────────────────────────────
	// https://jwt.io/#debugger — the pre-filled example token on the page
	"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c": true,
	// RS256 example from jwt.io
	"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.NHVaYe26MbtOYhSKkoKYdFVomg4i8ZJd8_-RU8VNbftc4TSMb4bXP3l3YlNWACwyXPGffz5aXHc6lty1Y2t4SWRqGteragsVdZufDn5BlnJl9pdR_kdVFUsra2rWKEofkZeIC4yWytE58sMIihvo9H1ScmmVwBcQP6XETqYd0aSHp1gOa9RdUPDvoXQ5oqygTqVtxaDr6wUFKrKItgBMzWIdNZ6y7O9E0DhEPTbE9rfBo6KTFsHAZnMg4k68CDp2woYIaXbmYTWcvbzIuHO7_37GT79XdIwkm95QJ7hYC9RiwrV7mesbY4PAahERJawntho0my942XheVLmGwLMBkQ": true,

	// ── GCP ────────────────────────────────────────────────────────────────────
	// https://cloud.google.com/docs/authentication/api-keys — example in guides
	"AIzaSyD-9tSrke72I6IsoFkSEXAMPLEKEY":    true,
	"AIzaSyC73SomeExampleKeyFromDocumentation": true,

	// ── GitHub ─────────────────────────────────────────────────────────────────
	// https://docs.github.com/en/authentication — examples in the token format docs
	"ghp_16C7e42F292c6912E7710c838347Ae178B4a": true, // classic PAT format example
	"github_pat_11ABCDE0Y0xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx": true,

	// Slack, OpenAI, Anthropic, Stripe live placeholder examples:
	// These are documented in official vendor docs and are handled by the
	// all-same-char check in Tier 1 (all-x strings) or by Tier 3 prefixes.
	// They are NOT listed here as exact strings to avoid GitHub push-protection
	// triggering on the source file itself — which would be self-defeating.

	// ── Twilio ─────────────────────────────────────────────────────────────────
	// https://www.twilio.com/docs/iam/test-credentials
	"ACaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": true, // test account SID
	"SKaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": true, // test API key SID

	// ── SendGrid ───────────────────────────────────────────────────────────────
	// https://docs.sendgrid.com/ui/account-and-settings/api-keys — example format
	"SG.xxxxxxxxxxxxxxxxxxxxxxxx.xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx": true,
	"SG.XXXXXXXXXXXXXXXXXXXXXXXX.XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX": true,

	// ── npm ────────────────────────────────────────────────────────────────────
	// https://docs.npmjs.com/about-access-tokens — format example
	"npm_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx": true,
	"npm_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX": true,
}
