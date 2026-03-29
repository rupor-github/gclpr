<h1>
    <img src="docs/clip.svg" style="vertical-align:middle; width:8%" align="absmiddle"/>
    <span style="vertical-align:middle;">&nbsp;&nbsp;gclpr</span>
</h1>

### Signed localhost clipboard and browser-open bridge
[![GitHub Release](https://img.shields.io/github/release/rupor-github/gclpr.svg)](https://github.com/rupor-github/gclpr/releases)

`gclpr` is a small utility that lets one process act as a local clipboard/browser service for another process.

It provides three client-side operations:

- `copy`: send text to the server clipboard
- `paste`: read text from the server clipboard
- `open`: ask the server to open a URL in its default browser

The transport is always TCP over `localhost`. If you want to use `gclpr` across machines, the intended setup is SSH local port forwarding so both ends still talk to `localhost`.

## Table of Contents

- [Why this exists](#why-this-exists)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Remote setup over SSH](#remote-setup-over-ssh)
- [Commands and options](#commands-and-options)
- [Alias behavior](#alias-behavior)
- [Open modes](#open-modes)
- [Debugging](#debugging)
- [Security model](#security-model)
- [Key files](#key-files)
- [URI validation](#uri-validation)
- [Windows tray application](#windows-tray-application)
- [Sample use cases](#sample-use-cases)
- [Compatibility notes](#compatibility-notes)
- [Implementation note](#implementation-note)

## Why this exists

`gclpr` was inspired by [lemonade](https://github.com/lemonade-command/lemonade), but focuses on a few practical constraints:

- Windows support matters, and Windows OpenSSH does not make Unix socket workflows convenient.
- Clipboard and browser-open requests should not be authorized by source IP alone.
- The tool should be easy to embed and reason about.

Instead of exposing a plaintext clipboard service to whoever can reach a port, every client request is signed with a private key and verified by the server against trusted public keys.

## Installation

`gclpr` is distributed as a single, zero-configuration binary with no external dependencies.

Windows users can install and update `gclpr` with [scoop](https://scoop.sh/):

```bash
scoop install https://github.com/rupor-github/gclpr/releases/latest/download/gclpr.json
scoop update gclpr
```

On all supported platforms, you can also download archives from the [releases page](https://github.com/rupor-github/gclpr/releases) and unpack them anywhere convenient. **Windows releases include both `gclpr.exe` (the standard CLI) and `gclpr-gui.exe` (a system tray application for running the server in the background).**

Starting with v1.1.1, release archives are zip files signed with [minisign](https://jedisct1.github.io/minisign/). Public key:

<p>
    <img src="docs/build_key.svg" style="vertical-align:middle; width:15%" align="absmiddle"/>
    <span style="vertical-align:middle;">&nbsp;&nbsp;RWTNh1aN8DrXq26YRmWO3bPBx4m8jBATGXt4Z96DF4OVSzdCBmoAU+Vq</span>
</p>

## Quick start

1. On the client side, create a key pair:

```bash
gclpr genkey
```

2. Copy the generated public key into the server trusted-keys file:

- Linux/macOS: `${HOME}/.gclpr/trusted`
- Windows: `${USERPROFILE}\.gclpr\trusted`

3. Start the server on the machine that owns the clipboard and browser you want to use:

Linux/macOS:

```bash
gclpr server
```

Windows:

```powershell
gclpr-gui.exe
```

On Windows, `gclpr-gui.exe` is the normal server entry point. Use `gclpr.exe` there as the client CLI.

4. Use the client commands:

```bash
gclpr copy 'hello'
gclpr paste
gclpr open 'https://example.com'
```

## Remote setup over SSH

`gclpr` always connects to `localhost`. To use a remote server, forward the remote server port to local `localhost` with SSH.

Example:

```bash
ssh -R 2850:localhost:2850 user@remote-host
```

Alternatively, you can configure this permanently in your `~/.ssh/config`:

```text
Host remote-host
  RemoteForward 2850 localhost:2850
```

```text
[ Client Machine ]                           [ Server Machine ]
gclpr client                                 gclpr server (port 2850)
(gclpr copy/open)                            (Clipboard / OS Browser)
       |                                              ^
       |             SSH Port Forwarding              |
  localhost:2850 ----------------------------> localhost:2850
```

With that tunnel in place:

- the local `gclpr` client still connects to `localhost:2850`
- the remote `gclpr server` still listens on its own `localhost:2850`
- SSH carries the traffic between them

This design is intentional. `gclpr` does not try to listen on non-loopback interfaces directly.

## Commands and options

Typical CLI help looks like this:

```text
gclpr [options]... COMMAND [arg]

Commands:
  copy 'text'   send text to server clipboard
  paste         output server clipboard locally
  open 'url'    open URL in server's default browser
  genkey        generate key pair for signing
  server        start server

Common options:
  -port int                 TCP port for the gclpr RPC server (default 2850)
  -connect-timeout duration TCP connect timeout and tunnel attach timeout
  -timeout duration         read/write I/O timeout and tunnel idle timeout
  -line-ending string       convert line endings for paste output (LF/CRLF)
  -tunnel                   tunnel an explicit loopback HTTP(S) URL
  -oauth                    tunnel the OAuth redirect_uri callback listener
  -debug                    enable debug logging
  -help                     show help
```

Important clarifications:

- `-port` is the `gclpr server` RPC port, not the browser-tunnel port.
- `-tunnel` and `-oauth` are mutually exclusive.
- `copy`, `paste`, and `open` are client commands; `server` is the long-running service.
- `open` without `-tunnel` or `-oauth` is a plain remote browser-open request with no callback tunnel.

## Alias behavior

`gclpr` can be invoked through compatible command names:

- `pbcopy` -> behaves like `gclpr copy`
- `pbpaste` -> behaves like `gclpr paste`
- `xdg-open` -> behaves like `gclpr open`

There is one special rule:

- when invoked as `xdg-open`, `gclpr` automatically enables OAuth mode unless `-tunnel` is explicitly requested

This is meant for tools such as Azure CLI that launch the browser through `xdg-open` and expect a localhost OAuth callback.

## Open modes

`open` supports three distinct behaviors.

### 1. Plain open

```bash
gclpr open 'https://example.com'
```

The client sends the URL to the server and the server opens it in its default browser.

Plain `open` also accepts bare hostnames such as `example.com` and normalizes them to `https://example.com` before launching the browser.

### 2. Explicit tunnel mode

```bash
gclpr open -tunnel 'http://localhost:3000'
```

Use this when the browser on the server must reach an HTTP(S) service that is available only on the client loopback interface.


```text
[ Client Machine ]                                [ Server Machine ]
gclpr open -tunnel --------(RPC setup)--------->  gclpr server
                                                  (binds localhost:PORT)
                                                          |
Local HTTP Service <----(Multiplexed Tunnel)----- OS Browser opens
(localhost:3000)                                  http://localhost:PORT
```


How it works:

1. The client asks the server to reserve a loopback listener.
2. The server opens a matching loopback port on its own machine.
3. The client attaches a tunnel worker to the server.
4. The server browser opens the loopback URL, and traffic is proxied back to the client loopback target.

Requirements and limits:

- only `http://` and `https://` URLs are accepted
- the target host must already be loopback: `localhost`, `127.0.0.1`, or `::1`
- this mode is strict; if setup fails, the command fails

Port conflicts:

- if the requested server-side loopback port is already busy, `gclpr` now falls back to a random available loopback port
- the URL opened in the browser is rewritten to use the actual bound port

### 3. OAuth tunnel mode

```bash
gclpr open -oauth 'https://login.example.com/auth?...&redirect_uri=http%3A%2F%2Flocalhost%3A3000%2Fcallback'
```

Use this when the authorization URL contains a localhost `redirect_uri` and the callback must reach a service listening on the client machine.


```text
[ Client Machine ]                                [ Server Machine ]
gclpr open -oauth ---------(RPC setup)--------->  gclpr server
(Original Exits)                                  (binds localhost:PORT)
       |                                                  |
(Worker Detaches)                                 (1. Browser authenticates
       |                                              with External IdP)
       |                                                  |
Local Callback   <------(Multiplexed Tunnel)----- OS Browser redirected to
(localhost:3000)                                  http://localhost:PORT/callback
```


How it works:

1. `gclpr` parses `redirect_uri` from the authorization URL.
2. It validates that the callback target is loopback HTTP(S).
3. It asks the server to reserve a matching loopback listener.
4. It launches a detached background worker that owns the tunnel.
5. The original process returns as soon as the worker is attached.
6. The server browser opens the authorization URL.

Behavior differences from `-tunnel`:

- the opened URL is usually not itself a loopback URL; only `redirect_uri` is tunneled
- setup is best-effort when invoked as `xdg-open`; if OAuth preparation fails, `gclpr` falls back to a normal remote open
- when `-oauth` is requested explicitly, the same best-effort fallback behavior is used by the current implementation

Port conflicts in OAuth mode:

- if the callback port from `redirect_uri` is unavailable on the server, `gclpr` chooses a random available loopback port
- the `redirect_uri` inside the opened authorization URL is rewritten to that actual port before the browser is launched

Example with Azure CLI on Linux:

```bash
export BROWSER=xdg-open
az login -t <tenant>
```

Debugging aliased browser flow:

```bash
GCLPR_DEBUG=1 BROWSER=xdg-open az login -t <tenant>
```

## Debugging

- `-debug` enables verbose logging
- `GCLPR_DEBUG=1` enables the same logging through the environment
- `GCLPR_DEBUG=1` is especially useful for aliased flows such as `xdg-open`, where you may not control the full command line
- in debug mode, detached OAuth workers write logs to a temporary file named `gclpr-worker-*.log`; the parent process prints the exact path before detaching

## Security model

`gclpr` authenticates requests but does not encrypt the transport on its own.

- every client request is signed with the client's private key
- the server verifies the signature against a trusted public key
- the wire protocol uses the SHA-256 hash of the public key as the request identity header
- if you need confidentiality across machines, use SSH port forwarding or another secure transport

This means:

- unauthorized clients should not be able to send clipboard or browser-open requests unless they possess a trusted private key
- traffic is still plaintext unless protected externally
- `gclpr` is designed around localhost exposure plus external tunneling if needed

## Key files

Client key material is stored in:

- Linux/macOS: `${HOME}/.gclpr`
- Windows: `${USERPROFILE}\.gclpr`

The server reads trusted public keys from the `trusted` file in that directory.

Format of `trusted`:

- plain text
- one hex-encoded public key per line
- lines beginning with `#` are comments

Requests are rejected when:

- the client key is not listed in `trusted`
- the request signature does not verify
- the protocol version is incompatible

`gclpr` also attempts to validate file permissions on key files, similar in spirit to OpenSSH.

## URI validation

The plain `open` command validates URIs before sending them to the OS opener.

- dangerous schemes such as `file:`, `data:`, `javascript:`, and `vbscript:` are blocked
- local filesystem-style paths are rejected, including Unix paths, Windows drive paths, UNC paths, and scheme-relative paths
- bare hostnames such as `example.com` are accepted and normalized to `https://example.com`
- loopback tunnel targets use stricter validation and must be absolute `http://` or `https://` URLs on loopback hosts only

## Windows tray application

On Windows, use `gclpr-gui.exe` as the server entry point. It runs the server as a notification tray application to simplify lifecycle management.

Use `gclpr.exe` for client-side commands such as `copy`, `paste`, and `open`.

Packages also include a prebuilt [npiperelay.exe](https://github.com/jstarks/npiperelay) to reduce extra setup for WSL2 workflows.

## Sample use cases

The examples below assume the Windows release is unpacked into a stable Windows-side location that is visible from WSL, for example `$HOME/winhome/.wsl/`, and that `gclpr` communication is already working, including SSH port forwarding where required. Adjust that path if you expose Windows files somewhere else.

Case 1 and Case 2 can happily coexist: the same Windows `gclpr-gui.exe` server can serve local WSL2 sessions and remote SSH sessions at the same time.

### Case 1. Multiple WSL2 sessions, one Windows server

Run `gclpr-gui.exe` on Windows and let every WSL2 shell or editor call the Windows-side `gclpr.exe`. This keeps one clipboard/browser server on Windows while multiple WSL2 sessions share it.

For shell-driven browser opens from WSL, add this to `~/.bashrc` or `~/.zshrc`:

```bash
export GCLPR_WIN="$HOME/winhome/.wsl/gclpr.exe"
export BROWSER="$GCLPR_WIN open"
```

This is an explicit `open` command path for tools that honor `BROWSER`.

For Neovim inside WSL, point the clipboard provider at the same Windows-side binary:

```lua
local gclpr = ""
if vim.fn.executable("gclpr") == 1 then -- clipboard provider https://github.com/rupor-github/gclpr
	gclpr = "gclpr"
	if vim.env.WSL_DISTRO_NAME then
		-- We are inside WSL - reach out to the Windows side.
		gclpr = vim.env.HOME .. "/winhome/.wsl/gclpr.exe"
	end
end

if #gclpr > 0 then
	vim.g.clipboard = {
		name = gclpr,
		paste = {
			["*"] = string.format("%s paste --line-ending lf", gclpr),
			["+"] = string.format("%s paste --line-ending lf", gclpr),
		},
		copy = {
			["*"] = string.format("%s copy", gclpr),
			["+"] = string.format("%s copy", gclpr),
		},
		cache_enabled = true,
	}
end
```

For `tmux` inside WSL, add this to `~/.tmux.conf`:

```tmux
set -s copy-command "~/winhome/.wsl/gclpr.exe copy"
```

In this setup, the Windows side remains the only server, Neovim opens links in the Windows-side browser, and the system clipboard is shared between the Windows host and all WSL2 sessions through the Windows client binary.

### Case 2. Multiple SSH sessions with port forwarding

With SSH port forwarding configured as described above, you can keep `gclpr-gui.exe` running on Windows and use the normal Linux `gclpr` binary from multiple SSH sessions.

For shell-driven browser opens on the Linux side, add this to `~/.bashrc` or `~/.zshrc`:

```bash
export BROWSER="gclpr open"
```

With the same Neovim clipboard configuration shown above, `gclpr` resolves to the Linux binary in SSH sessions and uses the forwarded `localhost:2850` connection to reach the Windows server.

For `tmux` in SSH sessions, add this to `~/.tmux.conf`:

```tmux
set -s copy-command "gclpr copy"
```

For OAuth-style flows such as `az login`, you can create an `xdg-open` symbolic link to `gclpr` somewhere in your user `PATH` and set `BROWSER=xdg-open`. That lets headless remote environments transparently route browser launches through `gclpr`, which then handles the localhost callback tunnel and completes authentication in the Windows-side browser. You can also call `gclpr open -oauth` explicitly when you want the same behavior without relying on the alias.

In this setup, links still open in the Windows-side browser, and the system clipboard is shared between the Windows host and all forwarded SSH sessions, including `tmux` copy operations.

## Compatibility notes

Breaking changes in older releases:

- v1.1.0 introduced a protocol signature with version checking and replaced the raw public key on the wire with its SHA-256 hash
- v2.0.0 introduced a 4-byte frame length prefix
- v2.1.0 added explicit localhost tunneling and OAuth handling modes.
- v2.2.0 changed the internal detached OAuth worker startup handshake to exchange the session id and MAC key over the startup TCP connection instead of command-line flags.

As a result, versions older than those protocol changes are not wire-compatible with newer versions.

The `v2.2.0` change affects only the internal parent-to-worker startup protocol used by `internal-oauth-worker`; normal client/server RPC and tunnel protocol compatibility is unchanged.

## Implementation note

`gclpr` uses public-key cryptography from Go's [NaCl implementation](https://pkg.go.dev/golang.org/x/crypto/nacl).
