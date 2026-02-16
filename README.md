<h1>   
    <img src="docs/clip.svg" style="vertical-align:middle; width:8%" align="absmiddle"/>
    <span style="vertical-align:middle;">&nbsp;&nbsp;gclpr</span>
</h1>

### Simple utility tool - copy/paste text and open URL in a browser over localhost TCP.
[![GitHub Release](https://img.shields.io/github/release/rupor-github/gclpr.svg)](https://github.com/rupor-github/gclpr/releases)

Installation
------------

Starting with v1.1.2 gclpr on Windows can be installed and updated using [scoop](https://scoop.sh/).

Installing:
```
    scoop install https://github.com/rupor-github/gclpr/releases/latest/download/gclpr.json
```
and updating:
```
    scoop update gclpr
```

Alternatively (for all supported platforms) download from the [releases page](https://github.com/rupor-github/gclpr/releases) and unpack it in a convenient location.

Starting with v1.1.1 releases are packed with zip and signed with [minisign](https://jedisct1.github.io/minisign/). Here is public key for verification:

<p>
    <img src="docs/build_key.svg" style="vertical-align:middle; width:15%" align="absmiddle"/>
    <span style="vertical-align:middle;">&nbsp;&nbsp;RWTNh1aN8DrXq26YRmWO3bPBx4m8jBATGXt4Z96DF4OVSzdCBmoAU+Vq</span>
</p>

Raison d'être
------------

I was using [lemonade](https://github.com/lemonade-command/lemonade) and particularly [my fork of it](https://github.com/rupor-github/lemonade) day to day 
for a while, but was never fully satisfied. In addition Lemonade code base was not structured for easy embedding in other projects.

Since native Windows support is required and Windows OpenSSH does not work with UNIX sockets (even if Windows itself supports them natively) TCP must be used and additional security measures should be taken. This was never lemonade's strong point - controlling access to clipboard by using source IP address does not seem good enough and today with VMs and containers configuration gets especially convoluted.

Enter another remote clipboard tool
------------
`gclpr` attempts to behave similar to `lemonade`:

```
gclpr - copy, paste text and open browser over localhost TCP interface

Version:
    1.0.0 (go version) git hash

Usage:
    gclpr [options]... COMMAND [arg]

Commands:

    copy 'text'  - (client) send text to server clipboard
    paste        - (client) output server clipboard locally
    open 'url'   - (client) open url in server's default browser
    genkey       - (client) generate key pair for signing
    server       - start server

Options:

  -debug
        Print debugging information
  -help
        Show help
  -line-ending string
        Convert Line Endings (LF/CRLF)
  -port int
        TCP port number (default 2850)
```
You could replace `pbcopy`, `pbpaste` and `xgd-open` with `gclpr` aliases - it will recognize names. Note, that TCP address cannot be changed (unlike in lemonade) - both client and server are always using `localhost`, only port could vary.

Recent Windows versions also include `gclpr-gui.exe` tools which allows you to run `server` command as notification tray icon on Windows simplifying life cycle management and packages pre-built [npiperelay.exe](https://github.com/jstarks/npiperelay) to avoid "scoop" installing additional packages needed by WSL2.

Each request from the client is being signed using **private** key from the previously generated pair and prepended with **public** key from this pair. Thus channel is not encrypted (this part should be taken care of by ssh if any remote access is required, along with local port redirection) but rather cryptographically verified. Without ssh redirection (or dirty firewall tricks) this is strictly local (localhost only) command line clipboard provider.

** BREAKING CHANGE ** Starting with v1.1.0 buffer sent to the server has 8 bytes signature (string "gclpr", 1 byte major version, 1 byte minor version, 1 byte patch version). Server checks first 6 bytes of signature (that includes major version check) and rejects incompatible requests. In addition instead of sending **public** key its **sha256 hash** is being used on a wire (it has exactly the same size). As the result all versions of gclpr older than 1.1.0 are incompatible with later versions (and vice versa) - if your clipboard stopped working please upgrade. In the future only major version change will break compatibility. 

** BREAKING CHANGE ** Starting with v2.0.0 buffers exchanged have 4 bytes length field at the begining. As the result all versions of gclpr older than 2.0.0 are incompatible with later versions (and vice versa) - if your clipboard stopped working please upgrade.

On the "client" end keys are created by issuing `genkey` command. Newly created keys are placed in `${HOME}/.gclpr` directory (`${USERPROFILE}\.gclpr` on Windows).

Server reads list of all known **public** keys from `${HOME}/.gclpr/trusted` file (`${USERPROFILE}\.gclpr\trusted` on Windows). Any request with _unknown_ public key will be denied by server. Any request failing signature _verification_ will be denied by server. Thus as long as private keys are not compromised it should be pretty difficult to access clipboard or send URI to `open` maliciously using network interfaces.

File with public keys is simple text file. It supports "#" comment at the beginning of the line. Each uncommented line contains single hex encoded public key and it is user responsibility to make sure that this file is up to date and reasonably secured. URIs are parsed by golang stdlib `ParaseRequestURI` before being sent to server to further limit potential damage.

Both client and server attempt to check permissions on key files on all platforms similarly to how it is done by OpenSSH code.

Project is using public-key cryptography from golang [implementation of NaCl](https://pkg.go.dev/golang.org/x/crypto/nacl). Any additional security could be easily afforded by using ssh to redirect local service port to other computers making clipboard fully global.
