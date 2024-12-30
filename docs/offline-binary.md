# Offline Binary

hiSHtory supports disabling syncing via `hishtory syncing disable`. This will disable persisting your (encrypted) history on the backend API server. For most users, this is the recommended option for running hiSHtory in an offline environment since it still supports opt-in updates via `hishtory update`.

But, if you need stronger guarantees that hiSHtory will not make any network requests, this can also be done by compiling your own copy of hiSHtory with the `offline` tag. This will statically link in `net_disabled.go` which will guarantee that the binary cannot make any HTTP requests. To use this:

```
git clone https://github.com/ddworken/hishtory
cd hishtory
go build -tags offline
./hishtory install
```

This binary will be entirely offline and is guaranteed to never make any requests to `api.hishtory.dev`.