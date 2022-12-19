# hiSHtory: Better Shell History

`hishtory` is a better shell history. It stores your shell history in context (what directory you ran the command in, whether it succeeded or failed, how long it took, etc). This is all stored locally and end-to-end encrypted for syncing to to all your other computers. All of this is easily queryable via the `hishtory` CLI. This means from your laptop, you can easily find that complex bash pipeline you wrote on your server, and see the context in which you ran it. 

![demo](https://raw.githubusercontent.com/ddworken/hishtory/master/backend/web/landing/www/img/demo.gif)

## Getting Started

To install `hishtory` on your first machine:

```bash
curl https://hishtory.dev/install.py | python3 -
```

At this point, `hishtory` is already managing your shell history (for bash, zsh, and fish!). Give it a try with `hishtory query` and see below for more details on the advanced query features. 

Then to install `hishtory` on your other computers, you need your secret key. Get this by running `hishtory status`. Once you have it, you follow similar steps to install hiSHtory on your other computers:

```bash
curl https://hishtory.dev/install.py | python3 -
hishtory init $YOUR_HISHTORY_SECRET
```

Now if you run `hishtory query` on first computer, you can automatically see the commands you've run on all your other computers!

## Features

### Querying

There are two ways to interact with hiSHtory. 

1. Via pressing `Control+R` in your terminal. Search for a command, select it via `Enter`, and then have it ready to execute in your terminal's buffer. 
2. Via `hishtory query` if you just want to explore your shell history. 

Both support the same query format, see the below annotated queries:

| Query | Explanation |
|---|---|
| `psql` | Find all commands containing `psql` |
| `psql db.example.com` | Find all commands containing `psql` and `db.example.com` |
| `docker hostname:my-server` | Find all commands containing `docker` that were run on the computer with hostname `my-server` |
| `nano user:root` | Find all commands containing `nano` that were run as `root` |
| `exit_code:127` | Find all commands that exited with code `127` |
| `service before:2022-02-01` | Find all commands containing `service` run before February 1st 2022 |
| `service after:2022-02-01` | Find all commands containing `service` run after February 1st 2022 |

For true power users, you can even query in SQLite via `sqlite3 -cmd 'PRAGMA journal_mode = WAL' ~/.hishtory/.hishtory.db`. 

### Enable/Disable

If you want to temporarily turn on/off hiSHtory recording, you can do so via `hishtory disable` (to turn off recording) and `hishtory enable` (to turn on recording). You can check whether or not `hishtory` is enabled via `hishtory status`. 

### Deletion

`hishtory redact` can be used to delete history entries that you didn't intend to record. It accepts the same search format as `hishtory query`. For example, to delete all history entries containing `psql`, run `hishtory redact psql`. 

Alternatively, you can delete items from within the terminal UI. Press `Control+R` to bring up the TUI, search for the item you want to delete, and then press `Control+K` to delete the currently selected entry.

### Updating

To update `hishtory` to the latest version, just run `hishtory update` to securely download and apply the latest update. 

### Advanced Features

<details>
<summary>Changing the displayed columns</summary>

You can customize the columns that are displayed via `hishtory config-set displayed-columns`. For example, to display only the cwd and command:

```
hishtory config-set displayed-columns CWD Command
```

</details>

<details>
<summary>Custom Columns</summary>

You can create custom column definitions that are populated from arbitrary commands. For example, if you want to create a new column named `git_remote` that contains the git remote if the cwd is in a git directory, you can run:

```
hishtory config-add custom-columns git_remote '(git remote -v 2>/dev/null | grep origin 1>/dev/null ) && git remote get-url origin || true'
hishtory config-add displayed-columns git_remote
```

</details>

<details>
<summary>Disabling Control+R integration</summary>

If you'd like to disable the Control+R integration in your shell, you can do so by running `hishtory config-set enable-control-r false`. 

</details>

<details>
<summary>Filtering duplicate entries</summary>

By default, hishtory query will show all results even if this includes duplicate history entries. This helps you keep track of how many times you've run a command and in what contexts. If you'd rather disable this so that hiSHtory won't show duplicate entries, you can run:

```
hishtory config-set filter-duplicate-commands true
```

</details>

<details>
<summary>Offline Install</summary>

If you don't need the ability to sync your shell history, you can install hiSHtory in offline mode. 

Download the latest binary from [Github Releases](https://github.com/ddworken/hishtory/releases), and then run `./hishtory-binary install --offline` to install hiSHtory in a fully offline mode. This disables syncing and it is not possible to re-enable syncing after doing this.

</details>

<details>
<summary>Self-Hosting</summary>

By default, hiSHtory relies on a backend for syncing. All data is end-to-end encrypted, so the backend can't view your history. 

But if you'd like to self-host the hishtory backend, you can! The backend is a simple go binary in `backend/server/server.go`. It can either use SQLite or Postgres for persistence. 

Check out the [`docker-compose.yml`](https://github.com/ddworken/hishtory/blob/master/backend/server/docker-compose.yml) file for an example config to start a hiSHtory server using postgres.

A few configuration options:

* If you want to use a SQLite backend, you can do so by setting the `HISHTORY_SQLITE_DB` environment variable to point to a file. It will then create a SQLite DB at the given location.
* If you want to limit the number of users that your server allows (e.g. because you only intend to use the server for yourself), you can set the environment variable `HISHTORY_MAX_NUM_USERS=1` (or to whatever value you wish for the limit to be). Leave it unset to allow registrations with no cap.

</details>

<details>
<summary>Importing existing history</summary>

hiSHtory imports your existing shell history by default. If for some reason this didn't work (e.g. you had your shell history in a non-standard file), you can import it by piping it into `hishtory import` (e.g. `cat ~/.my_history | hishtory import`).

</details>

<details>
<summary>Custom timestamp formats</summary>

You can configure a custom timestamp format for hiSHtory via `hishtory config-set timestamp-format '2006/Jan/2 15:04'`. The timestamp format string should be in [the format used by Go's `time.Format(...)`](https://pkg.go.dev/time#Time.Format). 

</details>

<details>
<summary>Customizing the install folder</summary>

By default, hiSHtory is installed in `~/.hishtory/`. If you want to customize this, you can do so by setting the `HISHTORY_PATH` environment variable to a path relative to your home directory (e.g. `export HISHTORY_PATH=.config/hishtory`). This must be set both when you install hiSHtory and when you use hiSHtory, so it is recommend to set it in your `.bashrc`/`.zshrc`/`.fishrc` before installing hiSHtory. 

</details>

<details>
<summary>Viewing debug logs</summary>

Debug logs are stored in `~/.hishtory/hishtory.log`. If you run into any issues, these may contain useful information.

</details>

<details>
<summary>Uninstalling</summary>

If you'd like to uninstall hishtory, just run `hishtory uninstall`. Note that this deletes the SQLite DB storing your history, so consider running a `hishtory export` first. 

Note that if you're uninstalling hishtory due to bad latency, try running `hishtory update` first! Latency has been improved over 100x since the first release so I'd highly recommend checking out the latest version. 

</details>

## Design

The `hishtory` CLI is written in Go. It hooks into the shell in order to track information about all commands that are run. It takes this data and saves it in a local SQLite DB managed via [GORM](https://gorm.io/). This data is then encrypted and sent to your other devices through a backend that essentially functions as a one-to-many queue. When you run `hishtory query`, a SQL query is run to find matching entries in the local SQLite DB. 

### Syncing Design 

See [hiSHtory: Cross-device Encrypted Syncing Design](https://blog.daviddworken.com/posts/hishtory-explained/) to learn how syncing works. The tl;dr is that everything magically works so that:

* The backend can't read your history. 
* Your history is queryable from all your devices. 
* You can delete items from your history as needed. 
* If you go offline, you'll have an offline copy of your history. And once you come back online, syncing will transparently resume.

## Security

`hishtory` is a CLI tool written in Go and uses AES-GCM for end-to-end encrypting your history entries while syncing them. The binary is reproducibly built and [SLSA Level 3](https://slsa.dev/) to make it easy to verify you're getting the code contained in this repository. 

This all ensures that the minimalist backend cannot read your shell history, it only sees encrypted data. 

If you find any security issues in hiSHtory, please reach out to `david@daviddworken.com`. 
