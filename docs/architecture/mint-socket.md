# On-Demand Mint Socket

## Why this exists

`mcp-token <service>` (see the [README section](../../README.md#mcp-token--on-demand-token-broker--keystore-cli)) already mints a fresh short-lived token on demand and prints it to stdout. The mint socket is the same idea wired to systemd socket activation, for consumers that cannot simply shell out to `mcp-token` themselves -- e.g. a sandboxed container that has no `mcp-token` binary, no keystore access, and no GitHub App private key, but does have a bind-mounted socket path.

The design intentionally avoids two other shapes:

- **A long-lived daemon holding a cached token.** The daemon would itself be a thing that can leak a live token, and it has to re-mint on some schedule anyway.
- **A wall-clock refresh timer** (systemd `OnCalendar=`/`OnUnitActiveSec=`) that periodically writes a token to a file. None of the machines this runs on (a WSL host whose Windows side sleeps, a GCP VM that auto-stops) stay up continuously, so a monotonic/wall-clock timer always has a window right after resume where the last-written token has already expired and the timer hasn't fired yet.

Socket activation sidesteps both: nothing runs between connections, and the token is minted at the moment it's needed, not on a schedule. See masuda-masuo/shiori#204 for the original cross-repo design discussion and masuda-masuo/mcp-launcher#25 for the `mcp-token` CLI this reuses.

## The pieces

| File | Role |
|---|---|
| [`systemd/mcp-token.socket`](../../systemd/mcp-token.socket) | Listens on `%t/mcp-token/mint.sock` (`%t` = `/run/user/<uid>` in user scope). `Accept=yes`: one service instance per connection. |
| [`systemd/mcp-token@.service`](../../systemd/mcp-token@.service) | Runs `mcp-token github` with stdin/stdout wired to the accepted connection. `StandardError=journal` keeps diagnostics out of the token stream. |
| [`scripts/install-mint-socket.sh`](../../scripts/install-mint-socket.sh) | Resolves/installs the `mcp-token` binary and its `launcher.json` (`--bin` / `--config`, see "Configuration file resolution" below), installs both units into `~/.config/systemd/user/`, and enables the socket. |

## Installing without a checkout (the kit)

The installer copies the unit files out of `systemd/` rather than embedding
them, so it is not a `curl | bash` script on its own. To install on a machine
that has no checkout and no Go toolchain — a headless server, the dev-infra
VM — take the **mint-socket kit** instead: every `mcp-token/vX.Y.Z` release
publishes `mcp-token-mint-socket-<version>.tar.gz`, which unpacks to the same
`scripts/` + `systemd/` layout the installer expects and is listed in the
release's `checksums.txt`.

The installer's `PINNED_VERSION` points at the release it ships in, so a kit
unpacked from release X installs the `mcp-token` binary from release X, and the
binary download is verified against that same `checksums.txt`. Nothing in this
path needs a GitHub token — which is the point, since this is the tool that
mints tokens (see "Security boundary"). Bump `PINNED_VERSION` in the commit
that is about to be tagged, never after.

See the README (`On-demand mint socket`) for the exact commands.

## The socket contract

1. A client connects to the socket.
2. The server (a freshly spawned `mcp-token github` process) writes a GitHub token to the connection and closes it. There is no request payload -- **connecting is the request.**
3. The client reads until EOF and strips surrounding whitespace (`mcp-token`'s output is `fmt.Fprintln(out, token)`, i.e. the token plus a trailing newline).

That's the entire protocol. No framing, no JSON, no auth handshake -- the socket's filesystem permissions (below) are the access control.

## Configuration file resolution

`mcp-token` (and the launcher) resolve `launcher.json` via
`internal/config.DefaultPath()`, shared by every binary in this
module:

1. `MCP_LAUNCHER_CONFIG` environment variable (explicit override)
2. the directory of the running executable (`os.Executable()` +
   `launcher.json`)
3. the current working directory (last-resort fallback)

Under plain interactive use, rule 2 is fine: you run `mcp-token` from
wherever you installed it, and its `launcher.json` sits right next to
it. That's the layout the GCP VM uses (`/opt/mcp-launcher/bin/{mcp-token,
launcher.json}`) and it's why `DefaultPath()` itself is not changed
by the mint socket work -- other consumers still depend on it.

Socket activation breaks that assumption. `mcp-token@.service` invokes
the binary with no shell, no cwd of the caller's choosing, and no
control over rule 3. Rule 2 becomes the only thing deciding "which
config" -- and therefore which GitHub App key / keystore entry -- gets
used, and that decision is now made by whichever of the resolution
steps in `scripts/install-mint-socket.sh --bin` (`--bin` /
`$MCP_TOKEN_EXE` / `$PATH` / `~/.local/bin/mcp-token` / a fresh pinned
download) happened to find a binary first. A fresh install downloading
to `~/.local/bin/mcp-token` has no `launcher.json` there at all, so
rule 2 fails outright and the socket accepts connections that always
fail to mint (`loading config: ... launcher.json: no such file`),
discovered only at first connection, not at install time.

`scripts/install-mint-socket.sh --config PATH` sidesteps this by
setting `Environment=MCP_LAUNCHER_CONFIG=<path>` explicitly in the
installed `mcp-token@.service`, i.e. by pinning rule 1 at install time
instead of leaving the service to fall through to rule 2. The
installer does this unconditionally, even when `--config` is omitted
and it finds a `launcher.json` next to the resolved binary (the same
directory rule 2 would land on) -- it writes that resolved path into
`Environment=` explicitly rather than leaving the variable unset. That
way `systemctl --user cat mcp-token@.service` alone tells you which
config a running instance uses, without also having to go check where
its binary happens to live. If neither `--config` nor an adjacent
`launcher.json` is found, the installer fails at install time rather
than installing a socket that is silently broken from the first
connection onward.

## Consumers

Read `GITHUB_TOKEN_SOCKET=/run/user/<uid>/mcp-token/mint.sock` (or `$XDG_RUNTIME_DIR/mcp-token/mint.sock`), connect, read to EOF, use the result as the token. `shiori` PR#242 has a from-scratch implementation of a push-model consumer; a consumer adapting to this pull model should be a *smaller* unit than that PR -- no timer, no cached-file staleness handling, just "open socket, read, use, repeat next time a token is needed." `sunaba` (code-sandbox-mcp) is expected to follow the same pattern for its `GITHUB_TOKEN_COMMAND`-style injection.

## Security boundary

**Whoever can connect to the socket gets a live GitHub token.** There is no further authentication at the socket layer. This is safe because:

- The socket lives under `%t` (`/run/user/<uid>`), which is created `0700` by systemd/pam -- other local users cannot traverse into it, let alone connect.
- `SocketMode=0600` on the listening socket itself is a second, redundant layer of the same restriction.
- The private key backing the mint never leaves the OS keystore; only the short-lived (~1h) installation token that `mcp-token github` mints is ever written to the socket.

**Do not mount this socket into a sandbox container (sunaba or otherwise).** A container that can connect to the mint socket can mint tokens for as long as the socket exists, with no scoping beyond whatever the GitHub App installation itself grants. If a sandboxed workflow needs a token, mint it host-side (e.g. via `sunaba`'s existing host-side credential resolution) and hand the *token* to the container -- never the socket.

## Consumer footgun: bind-mounting the socket into a container (Docker Desktop + WSL, measured)

These were hit empirically wiring a consumer up to a socket like this one and are recorded here so the next consumer doesn't rediscover them the hard way:

- Bind-mounting the socket itself works: a container can `connect()` to a bind-mounted Unix socket and receive data, whether the socket lives under `/home/...` or under `/run/user/<uid>/...`.
- **However, bind-mounting the socket as a single file pins its inode.** The moment the host process recreates the socket (which systemd does routinely -- e.g. across a `daemon-reload` or a service restart), the container's mount still points at the old, now-dead inode. The container's end of the mount starts returning `ECONNREFUSED` permanently, and nothing short of recreating the container fixes it.
  - **Mitigation: bind-mount the parent *directory*, not the socket file.** A directory bind mount re-resolves the socket file on every connection attempt, so socket recreation on the host is transparent to the container.
- **If the socket path doesn't exist yet when a file-level bind mount is requested, Docker silently creates a root-owned directory at that path on the host.** Once that happens, systemd can no longer bind the socket there (the path is now a directory, and it's not writable by the systemd-user-managed process), and the fix requires manually `rmdir`-ing the accidental directory.
  - This is exactly why `mcp-token.socket` uses `%t/mcp-token/mint.sock` with `DirectoryMode=0700`: systemd owns and creates `%t/mcp-token/` itself before the socket exists, so there's no window where a bind-mount tool can race it into creating the wrong thing there.

## Headless keyring unlock (out of scope here, noted for completeness)

On a machine with no interactive login (a bare GCP VM, a WSL instance nobody has opened a desktop session in), `mcp-token`'s underlying keystore calls into `gnome-keyring-daemon`, which needs its login keyring unlocked. Feeding it a **zero-byte** stdin fails to create a fresh login keyring; feeding it an **empty passphrase followed by a newline** (`printf '\n'`) succeeds and creates one with an empty password. This was measured against dev-infra#5 and is not implemented by anything in this change -- it's a prerequisite for `gnome-keyring-daemon.service` (which `mcp-token@.service` depends on via `Requires=`/`After=`) to be unlockable at all on a fully headless box, and belongs to whatever provisions that box, not to this socket.
