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

Raison d'être
------------

I was using [lemonade](https://github.com/lemonade-command/lemonade) and particularly [my fork of it](https://github.com/rupor-github/lemonade) in my day to day activities 
for a while, but was never fully satisfied. Lemonade code base was never structured for easy embedding in other projects. For [wsl-ssh-agent](https://github.com/rupor-github/wsl-ssh-agent) I had to copy and restructure some server code, which is never a good idea. 

Since native Windows support is required and Windows OpenSSH does not work with UNIX sockets (even if Windows itself supports them natively) TCP must be used and additional security measures should be taken. This was never lemonade strong point - controlling access to clipboard by using source IP address does not seem good enough and today with VMs and containers configuration gets especially convoluted.

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

Note, that TCP address cannot be changed - both client and server are always using `localhost`, only port could vary. 

Each request from the client is being signed using **private** key from the pair generated by `genkey` command and prepended with **public** key from this pair. During `genkey` keys are placed in `${HOME}/.gclpr` directory (`${USEERPROFILE}\.gclpr` on Windows). Server reads list of known **public** keys from `${HOME}/.gclpr/trusted` file (`${USEERPROFILE}\.gclpr\trusted` on Windows). It is simple text file (supporting "#" comment at the beginning of the line(s)). Each uncommented line contains single hex encoded public key and it is user responcibility to make sure that this file is up to date and secured. Any request with _unknown_ public key will be denied by server. Any request failing signature _verification_ will be denied by server. Thus as long as private keys are not compromized it should be pretty dificult to access clipboard or send URI to `open` maliciously using network interfaces. URIs are parsed by golang sdlib ParaseRequestURI before being sent to server to further limit potential damage.

Project is using public-key cryptography from golang [implementation of NaCl](https://pkg.go.dev/golang.org/x/crypto/nacl). Any additional security could be easily afforded by using ssh to redirect local service port to other computers making clipboard fully global.
