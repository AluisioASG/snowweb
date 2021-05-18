<!--
SPDX-FileCopyrightText: 2021 Aluísio Augusto Silva Gonçalves <https://aasg.name>

SPDX-License-Identifier: MIT
-->

# SnowWeb – a webserver for Nix flakes

[![built with nix](https://img.shields.io/static/v1?logo=nixos&logoColor=white&label=&message=Built%20with%20Nix&color=41439a)](https://builtwithnix.org)

SnowWeb is a web server for static websites built from Nix flakes, featuring on-demand rebuilds and automatic HTTPS.

## Basic usage

To serve [my website] on a random port:

```console
tty1$ go install git.sr.ht/~aasg/snowweb/cmd/snowweb@latest
tty1$ snowweb git+https://git.sr.ht/~aasg/haunted-blog
INF performing initial build
INF changed site root path=/nix/store/ms9mr70swdksjbnpr2zax8fas8l7mimy-aasg-blog
INF server started address=[::1]:43939

tty2$ # on a different terminal
tty2$ http HEAD 'http://[::1]:43939'
HTTP/1.1 200 OK
Accept-Ranges: bytes
Cache-Control: public, max-age=0, proxy-revalidate
Content-Length: 13777
Content-Type: text/html; charset=utf-8
Etag: "sha256-MMTumPhkIypwX7n5uio/D2dzny4hS2P0oJjqmUT2Yp8="
Server: SnowWeb
Vary: Accept-Encoding
```

The server is backed by Go's [http.ServeContent], meaning it supports conditional requests (for caching) and ranges (for resuming large downloads).
The `ETag` is the computed over the contents of the entire website, so it won't change after a rebuild if the resulting files are the same.

```console
tty2$ http --headers 'http://[::1]:43939' 'If-None-Match:"sha256-MMTumPhkIypwX7n5uio/D2dzny4hS2P0oJjqmUT2Yp8="' | head -n 1
HTTP/1.1 304 Not Modified

tty2$ http --body 'http://[::1]:43939' 'If-Range:"sha256-MMTumPhkIypwX7n5uio/D2dzny4hS2P0oJjqmUT2Yp8="' 'Range:bytes=131-185'
<title>Home — aasg's most experimental weblog</title>
```

What path is being served can be consulted through the `/.snowweb/status` endpoint, which can respond with plain text or JSON:

```console
tty2$ http --body --json 'http://[::1]:43939/.snowweb/status'
{
    "ok": true,
    "path": "/nix/store/ms9mr70swdksjbnpr2zax8fas8l7mimy-aasg-blog"
}
```

To prevent the built website from being garbage-collected by Nix, it is possible to use Nix's profile mechanism.
Simply create a writeable directory SnowWeb can use and pass `--profile /my/profile/dir/site-profile-link` to `snowweb`.

## Non-nixified websites

SnowWeb does not currently support anything but Nix flakes.
This may change if there is demand.

## Listening addresses

By default, SnowWeb will listen on a random port bound to the IPv6 loopback address (`::1`).
You can specify a different address by passing the `--listen` option or using the `SNOWWEB_LISTEN` environment variable:

```console
tty1$ # listen on port 80 in all interfaces, like a real web server
tty1$ snowweb --listen 'tcp:[::]:80' ~/my-http-only-website-flake
```

You can also listen on a Unix domain socket instead of a TCP port, for example if you have another program acting as reverse proxy:

```console
tty2$ # SnowWeb serves a single website, but you can use something like
tty2$ # Caddy or HAProxy to forward requests to the correct socket.
tty2$ snowweb --listen 'unix:/run/snowweb/site1.invalid' 'git+https://git.invalid/sites.git?dir=site1'
tty3$ snowweb --listen 'unix:/run/snowweb/site2.invalid' 'git+https://git.invalid/sites.git?dir=site2'
```

When running under systemd, you can let it manage the listening socket by configuring a socket unit and passing `systemd:` as the listening address to SnowWeb.

```ini
# snowweb.socket
[Socket]
ListenStream=80

# snowweb.service
[Unit]
Requires=snowweb.socket
[Service]
Environment=SNOWWEB_LISTEN=systemd:
ExecStart=/path/to/snowweb github:AluisioASG/chirpingmustard.com
```

## HTTPS

If you already have a TLS keypair, you can pass it with the `--tls-certificate` and `--tls-key` options, or through the `SNOWWEB_TLS_CERTIFICATE` and `SNOWWEB_TLS_KEY` environment variables:

```console
tty1$ # create a new keypair for the web server
tty1$ CAROOT=server-ca mkcert -cert-file server.pem -key-file server.key localhost ::1

tty1$ snowweb git+https://git.sr.ht/~aasg/haunted-blog --tls-certificate server.pem --tls-key server.key
INF performing initial build
INF changed site root path=/nix/store/ms9mr70swdksjbnpr2zax8fas8l7mimy-aasg-blog
INF server started address=[::1]:42303
INF performing initial TLS certificate management

tty2$ # on a different terminal
tty2$ http --verify server-ca/rootCA.pem --body 'https://[::1]:42303/.snowweb/status'
ok
serving /nix/store/ms9mr70swdksjbnpr2zax8fas8l7mimy-aasg-blog
```

SnowWeb is also capable of provisioning certificates automatically from an ACME certificate authority.
To enable this feature, pass the list of domains the certificate will be allowed for as a comma-separated list to the `--tls-acme-domains` option or in the `SNOWWEB_TLS_ACME_DOMAINS` environment variable:

```console
tty1$ # SnowWeb defaults to Let's Encrypt, but you can use another CA
tty1$ export SNOWWEB_TLS_ACME_CA=https://ca.lorkep.dn42/acme/acme/directory
tty1$ export SNOWWEB_TLS_ACME_CA_ROOTS=/etc/nixos/data/cacerts/LORKEP-Server-RootE3.pem
tty1$ export SNOWWEB_TLS_ACME_EMAIL=noc@lorkep.dn42
tty1$ snowweb git+https://git.sr.ht/~aasg/haunted-blog --listen 'tcp:[::]:443' --tls-acme-domains cdrn3w.lorkep.dn42
INF performing initial build
INF changed site root path=/nix/store/ms9mr70swdksjbnpr2zax8fas8l7mimy-aasg-blog
INF server started address=[::]:443
INF performing initial TLS certificate management
…
INF certificate obtained successfully
```

Note that when HTTPS is enabled, SnowWeb does not serve plain HTTP.
If you want HTTP requests to be redirected to HTTPS, use a different server to do it.

## On-demand rebuilds

SnowWeb can apply updates to the website it's serving without downtime.
To demonstrate, let's create a simple local flake we can later edit:

```nix
# hello-world/flake.nix
{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs";
  outputs = { self, nixpkgs }: {
    # replace x86_64-linux with your architecture
    defaultPackage.x86_64-linux = nixpkgs.legacyPackages.x86_64-linux.runCommand "hello-world" { } ''
      mkdir -p $out
      echo '<!DOCTYPE html><title>SnowTest</title>Hello, v1 world!' >$out/index.html
    '';
  };
}
```

Then serve it:

```console
tty1$ snowweb ./hello-world
INF performing initial build
INF changed site root path=/nix/store/dh2xclcq7y34d9l8s4mbxlav58jxnps6-hello-world
INF server started address=[::1]:45545
```

A `nix build` can be triggered locally by sending `SIGUSR1` or `SIGHUP` to the server process.

```console
tty2$ sed -i 's/v1/v2/' hello-world/flake.nix
tty2$ pkill -USR1 snowweb

tty1$ # back on the server terminal
INF rebuilding website
INF changed site root path=/nix/store/07rg421vs1lr1gqzf21drfcrak35lrrr-hello-world
```

Rebuilds can also be requested using the `/.snowweb/reload` endpoint over HTTPS with client authentication.

```console
tty1$ # create a certificate for the client
tty1$ CAROOT=client-ca mkcert -cert-file client.pem -key-file client.key -client aasg

tty1$ # assume HTTPS is already set up through the environment
tty1$ snowweb ./hello-world --client-ca client-ca/rootCA.pem
INF performing initial build
INF changed site root path=/nix/store/07rg421vs1lr1gqzf21drfcrak35lrrr-hello-world
INF server started address=[::1]:41695
INF performing initial TLS certificate management

tty2$ # on a different terminal
tty2$ sed -i 's/v2/v3/' hello-world/flake.nix
tty2$ http --body POST 'https://[::1]:41695/.snowweb/reload' --cert client.pem --cert-key client.key
ok
serving /nix/store/rhjqyip493zyis27sl3mnc8ymzzzizam-hello-world

tty1$ # back on the server terminal
INF processing remote rebuild request address=[::1]:49896
INF authenticated client for remote command serial=9650642011051223301278907915224192368 url_path=/.snowweb/reload
INF changed site root path=/nix/store/rhjqyip493zyis27sl3mnc8ymzzzizam-hello-world
```

[http.servecontent]: https://golang.org/pkg/net/http/#ServeContent
[my website]: https://git.sr.ht/~aasg/haunted-blog

