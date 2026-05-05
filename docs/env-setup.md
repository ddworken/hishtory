# Environment Variables

This document describes all environment variables used by hiSHtory.

## AI Configuration (affects both client and server)

### `AI_API_KEY`
Generic AI API key that serves as a fallback when `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` are not set. Supports both OpenAI (`sk-proj-`) and Anthropic (`sk-ant-`) key prefixes. The client checks if this is set to decide whether to use the direct API or the hishtory proxy.

- **Default**: Empty (no key)

### `AI_API_MODEL`
Overrides the AI model used for completions. Takes priority over `OPENAI_API_MODEL`.

- **Default**: `"gpt-4o-mini"` for OpenAI, `"claude-haiku-4-5-20251001"` for Anthropic-only
- **Priority**: `AI_API_MODEL` > `OPENAI_API_MODEL`

### `ANTHROPIC_API_KEY`
API key for Anthropic/Claude models. Has priority over `AI_API_KEY` (which is a fallback). Auto-detected by the `sk-ant-` prefix.

- **Default**: Empty

### `OPENAI_API_KEY`
API key for OpenAI/GPT models. Primary key for OpenAI, with `AI_API_KEY` as fallback. Auto-detected by the `sk-proj-` prefix.

- **Default**: Empty

### `OPENAI_API_MODEL`
Override for the OpenAI model used in completions. Lower priority than `AI_API_MODEL`.

- **Default**: `"gpt-4o-mini"` (or `"claude-haiku-4-5-20251001"` if only Anthropic key is available)
- **Priority**: `AI_API_MODEL` > `OPENAI_API_MODEL`

## hiSHtory Server

### `HISHTORY_SERVER`
Overrides the hiSHtory server hostname. Used for self-hosted deployments and testing.

- **Default**: `"https://api.hishtory.dev"`

### `HISHTORY_SERVER_PORT`
Sets the port the hiSHtory server listens on.

- **Default**: `"8080"`

### `HISHTORY_ENV`
Determines if the server is running in production mode. Affects DB migration behavior and other production-specific logic like DeepClean.

- **Default**: Empty (not production)
- **Production value**: `"prod"`

### `HISHTORY_ENABLE_USAGE_STATS`
Enables usage data tracking on self-hosted hiSHtory servers. Normally only enabled in production or test environments.

- **Default**: Empty (disabled for self-hosted servers)
- **Any non-empty value**: Enables usage tracking

### `HISHTORY_POSTGRES_DB`
Overrides the PostgreSQL database connection string for self-hosted servers. Takes priority over the password-based connection string.

- **Default**: `postgresql://postgres:<POSTGRESQL_PASSWORD>@postgres:5432/hishtory?sslmode=disable`
- **Example**: `postgresql://user:pass@host:5432/dbname?sslmode=disable`

### `HISHTORY_SQLITE_DB`
Enables SQLite backend for self-hosted hiSHtory servers instead of PostgreSQL. Set to a file path.

- **Default**: Empty (uses PostgreSQL)
- **Example**: `~/hishtory.db`

### `POSTGRESQL_PASSWORD`
PostgreSQL password for self-hosted hiSHtory server database connection. Used to construct the default connection string.

- **Default**: None (HISHTORY_POSTGRES_DB is expected instead)

### `HISHTORY_SELF_HOSTED_DEEP_CLEAN`
When set, permanently deletes history entries and devices that have been offline for 90+ days. Without it, only counts and warns.

- **Default**: Empty (only warns, doesn't delete)
- **Any non-empty value**: Performs deep clean

## hiSHtory Client

### `HISHTORY_PATH`
Customizes the hiSHtory data directory location (relative to home). Must be set in shellrc before install.

- **Default**: `~/.hishtory/`
- **Note**: Must be a relative path (cannot start with `/`)

### `HISHTORY_SHELL_NAME`
Tells hiSHtory which shell is using it (bash, zsh, fish). Passed to the TUI so AI suggestions are shell-appropriate.

- **Default**: `"bash"`
- **Values**: `"bash"`, `"zsh"`, `"fish"`
- **Note**: Set automatically by the shell config files (`config.sh`, `config.zsh`, `config.fish`)

### `HISHTORY_TERM_INTEGRATION`
Signals that the TUI is being invoked from shell integration (Control+R). When set, prevents clearing the terminal by printing the initial query instead.

- **Default**: Empty (normal TUI mode)
- **Any non-empty value**: Shell integration mode
- **Note**: Set automatically by the shell config files

### `HISHTORY_SKIP_INIT_IMPORT`
Skips the automatic import of existing shell history during `hishtory install` or `hishtory init`.

- **Default**: Empty (auto-import is enabled if < 10 entries exist)
- **Any non-empty value**: Skips auto-import
- **Note**: Set to `"1"` in CI/testing to avoid flaky imports

### `HISHTORY_REDACT_FORCE`
Skips the user confirmation prompt when running `hishtory redact`.

- **Default**: Empty (requires user confirmation)
- **Values**: `"1"`, `"true"` (any non-empty value works)

### `HISHTORY_FORCE_CLIENT_VERSION`
Forces the update mechanism to download a specific client version. Primarily used in tests to simulate downgrade scenarios.

- **Default**: Empty (uses normal version resolution)
- **Example**: `"v2.0.0"`

### `HISHTORY_DISABLE_SLSA_ATTESTATION`
Disables SLSA (Software Supply Chain Levels for Software Artifacts) attestation verification during updates.

- **Default**: Empty (SLSA verification is enabled)
- **Value**: `"true"` to bypass

### `HISHTORY_ALLOW_DOWNGRADE`
Bypasses the downgrade check during SLSA binary verification.

- **Default**: Empty (downgrades are blocked)
- **Value**: `"true"` to allow

## Testing

### `HISHTORY_TEST`
Master test flag that enables test-specific behavior: ANSI color profiles, tweaked shell configs, test environment detection, and background history saving.

- **Default**: Empty (production behavior)
- **Any non-empty value**: Enables test mode
- **Note**: Set to `"1"` in all test invocations (Makefile, CI, integration tests)

### `HISHTORY_UPDATE_GOLDENS`
When set, updates golden test files instead of failing when output differs. Used to regenerate expected test outputs.

- **Default**: Empty (golden mismatches cause test failures)
- **Any non-empty value**: Updates golden files

### `HISHTORY_SIMULATE_NETWORK_ERROR`
Simulates network failures for testing offline/error handling behavior.

- **Default**: Empty (normal network behavior)
- **Any non-empty value**: Returns simulated network errors on API calls

### `HISHTORY_CPU_PROFILE`
Path to a file where CPU profile data should be written during `tquery` command execution. Used for performance profiling.

- **Default**: Empty (no profiling)
- **Example**: `/tmp/cpu.profile`

### `HISHTORY_FILTERED_TEST`
Disables golden file verification in post-test processing. Used when running targeted/filtered test suites.

- **Default**: Empty (golden checks are enabled)
- **Any non-empty value**: Skips golden checks

### `HISHTORY_COMPOSE_TEST`
Triggers extra DB migration code path in the server for Docker Compose integration testing.

- **Default**: Empty
- **Any non-empty value**: Enables extra migrations
- **Note**: Set to `"1"` in docker-compose test CI

## CI/CD

### `GITHUB_ACTION`
Detects whether code is running inside a GitHub Actions runner.

- **Default**: Empty (not in GitHub Actions)
- **Any non-empty value**: Running in GitHub Actions

### `GITHUB_ACTIONS`
Used in tests to require API keys when running in GitHub Actions CI.

- **Default**: Empty (not in GitHub Actions)
- **Any non-empty value**: Running in GitHub Actions

### `NUM_TEST_SHARDS`
Total number of test shards for parallel test execution. Used by `NumTestShards()` and `MarkTestForSharding()`.

- **Default**: Empty (no sharding; returns -1)
- **CI value**: `"5"`

### `CURRENT_SHARD_NUM`
Identifies which shard a test is running on during parallel test sharding. Used by `CurrentShardNumber()` and `MarkTestForSharding()`.

- **Default**: Empty (disables sharding; returns -1)
- **CI value**: Set to matrix test shard index (e.g., `0`, `1`, `2`, ...)

## S3 Storage

### `HISHTORY_S3_SECRET_ACCESS_KEY`
S3 secret access key for hiSHtory's S3 backend storage. Required when using S3 storage alongside an access key ID. Referenced in code as `HISHTORY_S3_SECRET_ACCESS_KEY`.

- **Default**: None (required for S3 backend)
- **Note**: If `HISHTORY_S3_ACCESS_KEY_ID` is set but this is not, an error is raised
