<p align="center">
    <h1 align="center">gclpr</h1>
    <p align="center">
		Simple utility tool - copy, paste and open browser over localhost TCP.
    </p>
    <p align="center">
        <a href="https://goreportcard.com/report/github.com/rupor-github/gclpr"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/rupor-github/gclpr" /></a>
    </p>
    <hr>
</p>


Installation
------------

Download from the [releases page](https://github.com/rupor-github/gclpr/releases) and unpack it in a convenient location.

Starting with v1.1.1 releases are packed with zip and signed with [minisign](https://jedisct1.github.io/minisign/). Here is public key for verification: ![key](docs/build_key.png) RWTNh1aN8DrXq26YRmWO3bPBx4m8jBATGXt4Z96DF4OVSzdCBmoAU+Vq

Raison d'être
------------

I was using [lemonade](https://github.com/lemonade-command/lemonade) and particularly [my fork of it](https://github.com/rupor-github/lemonade) in my day to day activities 
for a while, but was never fully satisfied. Lemonade code base was never structured for easy embedding in other projects. For [wsl-ssh-agent](https://github.com/rupor-github/wsl-ssh-agent) I had to copy and restructure some server code, which is never a good idea. 

Since native Windows support is required and Windows OpenSSH does not work with UNIX sockets (even if Windows itself supports them natively) TCP must be used and additional security measures should be taken. This was never lemonade's strong point - controlling access to clipboard by using source IP address does not seem good enough and today with VMs and containers configuration gets especially convoluted.

Enter another remote clipboard tool
------------
`gclpr` attempts to behave similar to `lemonade`:

```
gclpr - copy, paste and open browser over localhost TCP interface

Version:
    1.0.0 (go1.15.6) 

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

Each request from the client is being signed using **private** key from the previously generated pair and prepended with **public** key from this pair. Thus channel is not encrypted (this part should be taken care of by ssh if any remote access is required, along with local port redirection) but rather cryptographically verified. Without ssh redirection (or dirty firewall tricks) this is strictly local (localhost only) command line clipboard provider.

** NOTE BREAKING CHANGE ** Starting with v1.1.0 buffer sent to the server has 8 bytes signature (string "gclpr", 1 byte major version, 1 byte minor version, 1 byte patch version). Server checks first 6 bytes of signature (that includes major version check) and rejects incompatible requests. In addition instead of sending **public** key its **sha256 hash** is being used on a wire (it has exactly the same size). As the result all versions of gclpr older than 1.1.0 are incompatible with later versions (and vice versa) - if your clipboard stopped working please upgrade. In the future only major version change will break compatibility. 

On the "client" end keys are created by issuing `genkey` command. Newly created keys are placed in `${HOME}/.gclpr` directory (`${USEERPROFILE}\.gclpr` on Windows).

Server reads list of all known **public** keys from `${HOME}/.gclpr/trusted` file (`${USEERPROFILE}\.gclpr\trusted` on Windows). Any request with _unknown_ public key will be denied by server. Any request failing signature _verification_ will be denied by server. Thus as long as private keys are not compromised it should be pretty difficult to access clipboard or send URI to `open` maliciously using network interfaces.

File with public keys is simple text file. It supports "#" comment at the beginning of the line. Each uncomment line contains single hex encoded public key and it is user responsibility to make sure that this file is up to date and reasonably secured. URIs are parsed by golang stdlib `ParaseRequestURI` before being sent to server to further limit potential damage.

Both client and server attempt to check permissions on key files on all platforms similarly to how its is done be OpenSSH code.

Project is using public-key cryptography from golang [implementation of NaCl](https://pkg.go.dev/golang.org/x/crypto/nacl). Any additional security could be easily afforded by using ssh to redirect local service port to other computers making clipboard fully global.
