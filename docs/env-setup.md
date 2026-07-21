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

### `AI_API_NUMBER_COMPLETIONS`
Overrides the number of AI completions requested per query. Takes priority over `OPENAI_API_NUMBER_COMPLETIONS`. Non-numeric values are silently ignored.

- **Default**: Empty (uses the number requested by the caller)
- **Priority**: `AI_API_NUMBER_COMPLETIONS` > `OPENAI_API_NUMBER_COMPLETIONS`

### `OPENAI_API_NUMBER_COMPLETIONS`
Override for the number of AI completions requested per query. Lower priority than `AI_API_NUMBER_COMPLETIONS`.

- **Default**: Empty (uses the number requested by the caller)

### `AI_API_SYSTEM_PROMPT`
Overrides the system prompt sent to the AI API. Takes priority over `OPENAI_API_SYSTEM_PROMPT`.

- **Default**: Empty (uses the built-in system prompt that instructs the model to reply with just a shell command)
- **Priority**: `AI_API_SYSTEM_PROMPT` > `OPENAI_API_SYSTEM_PROMPT`

### `OPENAI_API_SYSTEM_PROMPT`
Override for the system prompt sent to the AI API. Lower priority than `AI_API_SYSTEM_PROMPT`.

- **Default**: Empty (uses the built-in system prompt)

## hiSHtory Server

### `HISHTORY_SERVER`
Overrides the hiSHtory server hostname. Used for self-hosted deployments and testing.

- **Default**: `"https://api.hishtory.dev"`

### `HISHTORY_SERVER_PORT`
Sets the port the hiSHtory server listens on.

- **Default**: `"8080"`

### `HISHTORY_ENV`
Determines if the server is running in production mode. In production mode, the server skips DB auto-migration and index creation on startup, enables usage data tracking, and disables the self-hosted deep clean job (which only runs when the server is neither in production nor in test mode).

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
When set, permanently deletes history entries and devices that have been offline for 90+ days. Without it, only counts and warns. The deep clean job only runs on self-hosted servers (not in production or test mode) and is a no-op on SQLite backends.

- **Default**: Empty (only warns, doesn't delete)
- **Any non-empty value**: Performs deep clean

### `HISHTORY_MAX_NUM_USERS`
Limits the maximum number of users that can register on the server. Non-numeric values are ignored.

- **Default**: Empty (unlimited)
- **Example**: `"10"`

### `DD_API_KEY`
DataDog API key. On the server it enables the DataDog profiler (production observability). It is also read by the post-test tooling (`client/posttest`) to export test metrics to DataDog.

- **Default**: Empty (no DataDog integration)

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
Skips the automatic import of existing shell history during `hishtory install` or `hishtory init`. During `install`, auto-import only happens when fewer than 10 entries are already stored; during `init`, it always happens unless this variable is set.

- **Default**: Empty (auto-import is enabled)
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

### `_hishtory_tui_color`
Communicates the terminal's color support level to the TUI as an int (the termenv profile). Needed because the Control+R integration is not hooked up to the main TTY, so the TUI cannot detect color support itself.

- **Default**: Empty (falls back to ANSI256)
- **Note**: Set automatically by the shell config files using `hishtory getColorSupport`; not meant to be set by hand

## Testing

### `HISHTORY_TEST`
Master test flag that enables test-specific behavior: the TUI forces the ANSI color profile, `hishtory install` tweaks the generated shell configs for tests, the zsh config force-loads compinit, and the server runs against an in-memory SQLite DB.

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

### `FORCED_BANNER`
Forwarded by the client to the server's `/api/v1/banner` endpoint as the `forced_banner` query param. Used to test banner display.

- **Default**: Empty (no forced banner)

### `FAST`
Skips slow tests in the client integration test suite.

- **Default**: Empty (all tests run)
- **Any non-empty value**: Skips slow tests

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
S3 secret access key for hiSHtory's S3 backend storage. The secret is intentionally never stored in the config file; it is always loaded from this environment variable.

- **Default**: Empty
- **Note**: If the `access_key_id` field is set in the S3 config file but this variable is not set, an error is raised. If neither is set, IAM-role/ambient AWS credentials can be used instead.

## Standard Variables

These are standard shell/system variables that hiSHtory reads but does not own.

### `ZDOTDIR`
Used during `hishtory install` to locate the `.zshrc` file to modify.

- **Default**: Empty (falls back to `~/.zshrc`)

### `HISTFILE`
Custom shell history file location, added to the list of files scanned by `hishtory import` (alongside `~/.bash_history`, `~/.zsh_history`, etc.).

### `HISTTIMEFORMAT`
Bash history timestamp format. Used when parsing bash history lines to strip the timestamp prefix from imported commands.

### `PWD`
Fallback for detecting the current working directory when saving a history entry, if `syscall.Getwd` fails.

### `TMPDIR`
Temporary directory used for downloading the new client binary during `hishtory update`.

- **Default**: `/tmp/`
