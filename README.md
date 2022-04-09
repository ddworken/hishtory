# hishtory: Better Shell Hishtory

`hishtory` is a CLI tool to better manage your shell history. It hooks into your shell to store the commands you run along with metadata about those commands (what directory you ran it in, whether it succeeded or failed, how long it took, etc). This is all stored in a local SQLite DB, and then e2e encrypted while synced to local SQLite DBs running on all your other computers. All of this is easily queryable via the `hishtory` CLI. This means from your laptop, you can easily find that complex bash pipeline you wrote on your server, and see the context in which you ran it. 

`hishtory` is written in Go and uses AES-GCM for end-to-end encrypting your hishtory entries while syncing them. The binary is reproducibly built and [SLSA Level 3](https://slsa.dev/) to make it easy to verify you're getting the code contained in this repository. 

## Getting Started

To install `hishtory` on your first machine:

```bash
curl -L -o hishtory https://api.hishtory.dev/download/hishtory-linux-amd64
# Optional: Verify the binarie's SLSA L3 attestations from https://api.hishtory.dev/download/hishtory-linux-amd64.intoto.jsonl
chmod +x hishtory
./hishtory install
```

At this point, `hishtory` is already persisting your shell history. Give it a try with `hishtory query` and see below for more details on the advanced query features. 

Then to install `hishtory` on your other computers, you need your secret key. Get this by running `hishtory status`. Once you have it, you follow similar steps to install hishtory on your other computers:

```bash
curl -L -o hishtory https://api.hishtory.dev/download/hishtory-linux-amd64
# Optional: Verify the binarie's SLSA L3 attestations from https://api.hishtory.dev/download/hishtory-linux-amd64.intoto.jsonl
chmod +x hishtory
./hishtory install $SECRET_KEY
```

Now if you run `hishtory query` on first computer, you can automatically see the commands you've run on all your other computers!

## Features

### Advanced Queries

### Export

### Enable/Disable

### update

## Design

### CLI 

### Syncing

## Security

## Pending Features

* zsh support
* mac support 
* automatic test running in github actions