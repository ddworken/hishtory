# hishtory: Better Shell Hishtory

`hishtory` is a better shell history. It stores your shell history in context (what directory you ran the command it, whether it succeeded or failed, how long it took, etc). This is all stored in a local SQLite DB and e2e encrypted while synced to local SQLite DBs running on all your other computers. All of this is easily queryable via the `hishtory` CLI. This means from your laptop, you can easily find that complex bash pipeline you wrote on your server, and see the context in which you ran it. 

`hishtory` is written in Go and uses AES-GCM for end-to-end encrypting your hishtory entries while syncing them. The binary is reproducibly built and [SLSA Level 3](https://slsa.dev/) to make it easy to verify you're getting the code contained in this repository. 

## Getting Started

To install `hishtory` on your first machine:

```bash
curl https://hishtory.dev/install.py | python3 -
```

At this point, `hishtory` is already persisting your shell history. Give it a try with `hishtory query` and see below for more details on the advanced query features. 

Then to install `hishtory` on your other computers, you need your secret key. Get this by running `hishtory status`. Once you have it, you follow similar steps to install hishtory on your other computers:

```bash
curl https://hishtory.dev/install.py | python3 -
hishtory init $YOUR_HISHTORY_SECRET
```

Now if you run `hishtory query` on first computer, you can automatically see the commands you've run on all your other computers!

## Features

### Querying

`hishtory query` is the main interface for searching through your shell history. See some below annotated queries:

| Command | Explanation |
|---|---|
| `hishtory query psql` | Find all commands containing `psql` |
| `hishtory query psql db.example.com` | Find all commands containing `psql` and `db.example.com` |
| `hishtory query docker hostname:my-server` | Find all commands containing `docker` that were run on the computer with hostname `my-server` |
| `hishtory query nano user:root` | Find all commands containing `nano` that were run as `root` |
| `hishtory query exit_code:127` | Find all commands that exited with code `127` |
| `hishtory query service before:2022-02-01` | Find all commands containing `service` run before February 1st 2022 |
| `hishtory query service after:2022-02-01` | Find all commands containing `service` run after February 1st 2022 |

For true power users, you can query via SQL via `sqlite3 ~/.hishtory/.hishtory.db`. 

In addition, `hishtory export` dumps all commands to stdout separated by a single line. This can be useful for certain advanced use cases. 

### Enable/Disable

If you want to temporarily turn on/off hishtory recording, you can do so via `hishtory disable` (to turn off recording) and `hishtory enable` (to turn on recording). You can check whether or not `hishtory` is enabled via `hishtory status`. 

### Deletion

`hishtory redact` can be used to delete history entries that you didn't intend to record. It accepts the same search format as `hishtory query`. For example, to delete all history entries containing `psql`, run `hishtory redact psql`. 

### Updating

To update `hishtory` to the latest version, just run `hishtory update` to transparently download and apply the latest update. 

### Multi-Shell Support

`hishtory` supports `zsh` and `bash`. If you'd like support for another shell (e.g. `fish`), please open an issue!

## Design

The `hishtory` CLI is written in Go. It hooks into the shell in order to track information about all commands that are run (specifically in bash this is done via `trap DEBUG` and overriding `$PROMPT_COMMAND`). It takes this data and saves it in a local SQLite DB managed via [GORM](https://gorm.io/). When the user runs `hishtory query`, a SQL query is run to find matching entries in the local SQLite DB. 

### Syncing Design 

When `hishtory` is installed, it generates a random secret key. Computers that share a history share this secret key (which is manually copied by the user). It deterministically generates three additional secrets from the secret key:

1. `UserId = HMAC(SecretKey, "user_id")`
2. `DeviceId = HMAC(SecretKey, "device_id")`
3. `EncryptionKey = HMAC(SecretKey, "encryption_key")`

At installation time, `hishtory` registers itself with the backend which stores the tuple `(UserId, DeviceId)` which represents a one-to-many relationship between user and devices. In addition, it creates a `DumpRequest` specific that a new device was created and it needs a copy of the existing bash history. 

When a command is run:

1. `hishtory` encrypts (via AES-GCM with `EncryptionKey`) the command (and all the metadata) and sends it to the backend along with the `UserId` to persist it for. The backend retrieves a list of all associated `DeviceId`s and stores a copy of the encrypted blob for each device associated with that user. Once a given device has read an encrypted blob, that entry can be deleted in order to save space (in essence this is a per-device queue, but implemented on top of postgres because this is small scale and I already am running a postgres instance). 
2. `hishtory` checks for any pending `DumpRequest`s, and if there are any sends a complete (encrypted) copy of the local SQLite DB to be shared with the requesting device. 

When the user runs `hishtory query`, it retrieves all unread blobs from the backend, decrypts them, and adds them to the local SQLite DB. 

## Security

Hishtory is designed to ensure that the backend cannot read your shell history. This is achieved by:

1. Encrypting history entries with a secret key that the backend never sees
2. Using [SLSA](https://slsa.dev/) to support secure updates that guarantee that you're running the code contained in this repo

If you find any security issues in hishtory, please reach out to `david@daviddworken.com`. 