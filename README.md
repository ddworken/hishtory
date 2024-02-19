# hiSHtory: Better Shell History

`hishtory` is a better shell history. It stores your shell history in context (what directory you ran the command in, whether it succeeded or failed, how long it took, etc). This is all stored locally and end-to-end encrypted for syncing to to all your other computers. All of this is easily queryable via the `hishtory` CLI. This means from your laptop, you can easily find that complex bash pipeline you wrote on your server, and see the context in which you ran it. 

![demo](https://raw.githubusercontent.com/ddworken/hishtory/master/backend/web/landing/www/img/demo.gif)

## Getting Started

To install `hishtory` on your first machine:

```bash
curl https://hishtory.dev/install.py | python3 -
```

At this point, `hishtory` is already managing your shell history (for bash, zsh, and fish!). Give it a try by pressing `Control+R` and see below for more details on the advanced search features. 

Then to install `hishtory` on your other computers, you need your secret key. Get this by running `hishtory status`. Once you have it, you follow similar steps to install hiSHtory on your other computers:

```bash
curl https://hishtory.dev/install.py | python3 -
hishtory init $YOUR_HISHTORY_SECRET
```

Now if you press `Control+R` on first computer, you can automatically see the commands you've run on all your other computers!

## Features

### Querying

You can then query hiSHtory by pressing `Control+R` in your terminal. Search for a command, select it via `Enter`, and then have it ready to execute in your terminal's buffer. Or just hit `Escape` if you don't want to execute it after all. 

Both support the same query format, see the below annotated queries:

| Query | Explanation |
|---|---|
| `psql` | Find all commands containing `psql` |
| `psql db.example.com` | Find all commands containing `psql` and `db.example.com` |
| `"docker run" hostname:my-server` | Find all commands containing `docker run` that were run on the computer with hostname `my-server` |
| `nano user:root` | Find all commands containing `nano` that were run as `root` |
| `exit_code:127` | Find all commands that exited with code `127` |
| `service before:2022-02-01` | Find all commands containing `service` run before February 1st 2022 |
| `service after:2022-02-01` | Find all commands containing `service` run after February 1st 2022 |

For true power users, you can even query directly in SQLite via `sqlite3 -cmd 'PRAGMA journal_mode = WAL' ~/.hishtory/.hishtory.db`. 

### Enable/Disable

If you want to temporarily turn on/off hiSHtory recording, you can do so via `hishtory disable` (to turn off recording) and `hishtory enable` (to turn on recording). You can check whether or not `hishtory` is enabled via `hishtory status`. 

### Deletion

`hishtory redact` can be used to delete history entries that you didn't intend to record. It accepts the same search format as `hishtory query`. For example, to delete all history entries containing `psql`, run `hishtory redact psql`. 

Alternatively, you can delete items from within the terminal UI. Press `Control+R` to bring up the TUI, search for the item you want to delete, and then press `Control+K` to delete the currently selected entry.

### Updating

To update `hishtory` to the latest version, just run `hishtory update` to securely download and apply the latest update. 

### Advanced Features

<details>
<summary>AI Shell Assistance</summary><blockquote>

If you are ever trying to figure out a shell command and searching your history isn't working, you can query ChatGPT by prefixing your query with `?`. For example, press `Control+R` and then type in `? list all files larger than 1MB`:

![demo showing ChatGPT suggesting the right command](https://raw.githubusercontent.com/ddworken/hishtory/master/backend/web/landing/www/img/aidemo.png)

If you would like to:
* Disable this, you can run `hishtory config-set ai-completion false`
* Run this with your own OpenAI API key (thereby ensuring that your queries do not pass through the centrally hosted hiSHtory server), you can run `export OPENAI_API_KEY='...'`

</blockquote></details>

<details>
<summary>TUI key bindings</summary><blockquote>

The TUI (opened via `Control+R`) supports a number of key bindings:

| Key                | Result                                                         |
|--------------------|----------------------------------------------------------------|
| Left/Right         | Scroll the search query left/right                             |
| Up/Down            | Scroll the table up/down                                       |
| Page Up/Down       | Scroll the table up/down by one page                           |
| Shift + Left/Right | Scroll the table left/right  |
| Control+K          | Delete the selected command                                    |

Press `Control+H` to view a help page documenting these.

</blockquote></details>

<details>
<summary>Changing the displayed columns</summary><blockquote>

You can customize the columns that are displayed via `hishtory config-set displayed-columns`. For example, to display only the cwd and command:

```
hishtory config-set displayed-columns CWD Command
```

The list of supported columns are: `Hostname`, `CWD`, `Timestamp`, `Runtime`, `ExitCode`, `Command`, and `User`.

</blockquote></details>

<details>
<summary>Custom Columns</summary><blockquote>

You can create custom column definitions that are populated from arbitrary commands. For example, if you want to create a new column named `git_remote` that contains the git remote if the cwd is in a git directory, you can run:

```
hishtory config-add custom-columns git_remote '(git remote -v 2>/dev/null | grep origin 1>/dev/null ) && git remote get-url origin || true'
hishtory config-add displayed-columns git_remote
```

</blockquote></details>

<details>
<summary>Custom Color Scheme</summary><blockquote>

You can customize hishtory's color scheme for the TUI. Run `hishtory config-set color-scheme` to see information on what is customizable and how to do so.

</blockquote></details>

<details>
<summary>Disabling Control+R integration</summary><blockquote>

If you'd like to disable the Control+R integration in your shell, you can do so by running `hishtory config-set enable-control-r false`. If you do this, you can then manually query hiSHtory by running `hishtory query <YOUR QUERY HERE>`.

</blockquote></details>

<details>
<summary>Filtering duplicate entries</summary><blockquote>

By default, hishtory query will show all results even if this includes duplicate history entries. This helps you keep track of how many times you've run a command and in what contexts. If you'd rather disable this so that hiSHtory won't show duplicate entries, you can run:

```
hishtory config-set filter-duplicate-commands true
```

</blockquote></details>

<details>
<summary>Offline Install Without Syncing</summary><blockquote>

If you don't need the ability to sync your shell history, you can install hiSHtory in offline mode:

```
curl https://hishtory.dev/install.py | HISHTORY_OFFLINE=true python3 -
```

This disables syncing and it is not possible to re-enable syncing after doing this.

</blockquote></details>

<details>
<summary>Self-Hosting</summary><blockquote>

By default, hiSHtory relies on a backend for syncing. All data is end-to-end encrypted, so the backend can't view your history. 

But if you'd like to self-host the hishtory backend, you can! The backend is a simple go binary in `backend/server/server.go` (with [prebuilt binaries here](https://github.com/ddworken/hishtory/tags)). It can either use SQLite or Postgres for persistence.

To make `hishtory` use your self-hosted server, set the `HISHTORY_SERVER` environment variable to the origin of your self-hosted server. For example, put `export HISHTORY_SERVER=http://my-hishtory-server.example.com` at the end of your `.bashrc`.

Check out the [`docker-compose.yml`](https://github.com/ddworken/hishtory/blob/master/backend/server/docker-compose.yml) file for an example config to start a hiSHtory server using Postgres.

A few configuration options:

* If you want to use a SQLite backend, you can do so by setting the `HISHTORY_SQLITE_DB` environment variable to point to a file. It will then create a SQLite DB at the given location.
* If you want to limit the number of users that your server allows (e.g. because you only intend to use the server for yourself), you can set the environment variable `HISHTORY_MAX_NUM_USERS=1` (or to whatever value you wish for the limit to be). Leave it unset to allow registrations with no cap.

</blockquote></details>

<details>
<summary>Importing existing history</summary><blockquote>

hiSHtory imports your existing shell history by default. If for some reason this didn't work (e.g. you had your shell history in a non-standard file), you can import it by piping it into `hishtory import` (e.g. `cat ~/.my_history | hishtory import`).

</blockquote></details>

<details>
<summary>Custom timestamp formats</summary><blockquote>

You can configure a custom timestamp format for hiSHtory via `hishtory config-set timestamp-format '2006/Jan/2 15:04'`. The timestamp format string should be in [the format used by Go's `time.Format(...)`](https://pkg.go.dev/time#Time.Format). 

</blockquote></details>

<details>
<summary>Web UI for sharing</summary><blockquote>

If you'd like to temporarily allow someone else to search your shell history, you can start a web server via `hishtory start-web-ui`. This will expose a basic web UI on port `8000` where they can query your history:

![demo showing the web UI searching for git](https://raw.githubusercontent.com/ddworken/hishtory/master/backend/web/landing/www/img/webui.png)

</blockquote></details>


<details>
<summary>Customizing the install folder</summary><blockquote>

By default, hiSHtory is installed in `~/.hishtory/`. If you want to customize this, you can do so by setting the `HISHTORY_PATH` environment variable to a path relative to your home directory (e.g. `export HISHTORY_PATH=.config/hishtory`). This must be set both when you install hiSHtory and when you use hiSHtory, so it is recommend to set it in your `.bashrc`/`.zshrc`/`.fishrc` before installing hiSHtory. 

</blockquote></details>

<details>
<summary>Viewing debug logs</summary><blockquote>

Debug logs are stored in `~/.hishtory/hishtory.log`. If you run into any issues, these may contain useful information.

</blockquote></details>

<details>
<summary>Uninstalling</summary><blockquote>

If you'd like to uninstall hishtory, just run `hishtory uninstall`. Note that this deletes the SQLite DB storing your history, so consider running a `hishtory export` first. 

Note that if you're experiencing any issues with hiSHtory, try running `hishtory update` first! Performance and reliability is always improving, and we highly value [your feedback](https://github.com/ddworken/hishtory/issues).

</blockquote></details>

## Design

The `hishtory` CLI is written in Go. It hooks into the shell in order to track information about all commands that are run. It takes this data and saves it in a local SQLite DB managed via [GORM](https://gorm.io/). This data is then encrypted and sent to your other devices through a backend that essentially functions as a one-to-many queue. When you press press `Control+R` or run `hishtory query`, a SQL query is run to find matching entries in the local SQLite DB. 

### Syncing Design 

See [hiSHtory: Cross-device Encrypted Syncing Design](https://blog.daviddworken.com/posts/hishtory-explained/) to learn how syncing works. The tl;dr is that everything magically works so that:

* The backend can't read your history. 
* Your history is queryable from all your devices. 
* You can delete items from your history as needed. 
* If you go offline, you'll have an offline copy of your history. And once you come back online, syncing will transparently resume.

## Contributing

Contributions are extremely welcome! I appreciate all contributions in terms of both issues (please let me know about any bugs you find!) and PRs. 

If you're making code contributions, check out `make help` for some information on some useful commands. Namely, note that my general dev workflow consists of:

* Make some local changes (e.g. to fix a bug or add a new feature)
* Run `make local-install` to build and install your local version (note that this won't mess up your current hishtory DB!)
* ... Repeat until you're happy with your change ...
* Write some tests for your change. Unit tests are great, but we also have a large number of integration tests in `client_test.go`
    * Note that the hishtory tests are quite thorough, so running them locally is quite time consuming (and some of them only work on Github Actions). Instead, I recommend using `make ftest` (see `make help` for information on this) to run the specific tests that you're adding/changing.
* Open a PR on Github! Once you open the PR, I'll take a look and will trigger Github Actions to run all the tests which will ensure that your change doesn't lead to any reggressions.
* [Optional] If you want to switch back to the latest released version (rather than your local change), run `hishtory update`
* Merge the PR! :tada:

## Security

`hishtory` is a CLI tool written in Go and uses AES-GCM for end-to-end encrypting your history entries and syncing them. The binary is reproducibly built and [SLSA Level 3](https://slsa.dev/) to make it easy to verify you're getting the code contained in this repository. 

This all ensures that the minimalist backend cannot read your shell history, it only sees encrypted data. hiSHtory also respects shell conventions and will not record any commands prefixed with a space.

If you find any security issues in hiSHtory, please reach out to `david@daviddworken.com`. 
