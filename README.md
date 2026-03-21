# Box

Box is a simple linux container runtime written in Go that is capable of running a variety of popular images. It uses the [OCI runtime-spec](https://specs.opencontainers.org/runtime-spec/?v=v1.0.2) but does not try to be compliant.

I built it as a way to learn more about certain aspects of containers and Linux:

 - OCI specs
 - namespaces
 - capabilities
 - virtual networking
 - cgroups + systemd

## Usage / Examples

### alpine

https://hub.docker.com/_/alpine

```
> sudo go run ./box pull "docker.io/library/alpine:latest" ./build/images/alpine/runtime --quiet

> sudo go run ./box run alpine-container ./build/images/alpine/runtime --quiet
/ # ls
bin    dev    etc    home   lib    media  mnt    opt    proc   root   run    sbin   srv    sys    tmp    usr    var

/ # cat /etc/os-release
NAME="Alpine Linux"
ID=alpine
VERSION_ID=3.23.3
PRETTY_NAME="Alpine Linux v3.23"
HOME_URL="https://alpinelinux.org/"
BUG_REPORT_URL="https://gitlab.alpinelinux.org/alpine/aports/-/issues"

/ # ping google.com
PING google.com (209.85.203.138): 56 data bytes
64 bytes from 209.85.203.138: seq=0 ttl=108 time=5.940 ms
64 bytes from 209.85.203.138: seq=1 ttl=108 time=5.904 ms
64 bytes from 209.85.203.138: seq=2 ttl=108 time=6.152 ms
^C
--- google.com ping statistics ---
3 packets transmitted, 3 packets received, 0% packet loss
round-trip min/avg/max = 5.904/5.998/6.152 ms
```

### ubuntu 24.04

https://hub.docker.com/_/ubuntu

Showcasing a bigger OS and imposing cpu + memory limits.


```
> sudo go run ./box pull "docker.io/library/ubuntu:24.04" ./build/images/ubuntu-24-04/runtime --quiet

> sudo go run ./box run --cpus 4 --mem 256 ubuntu-container ./build/images/ubuntu-24-04/runtime
root@box:/# cat /etc/os-release
PRETTY_NAME="Ubuntu 24.04.4 LTS"
NAME="Ubuntu"
VERSION_ID="24.04"
VERSION="24.04.4 LTS (Noble Numbat)"
VERSION_CODENAME=noble
ID=ubuntu
ID_LIKE=debian
HOME_URL="https://www.ubuntu.com/"
SUPPORT_URL="https://help.ubuntu.com/"
BUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"
PRIVACY_POLICY_URL="https://www.ubuntu.com/legal/terms-and-policies/privacy-policy"
UBUNTU_CODENAME=noble
LOGO=ubuntu-logo

root@box:/# apt update && apt install curl bind9-dnsutils
Get:1 http://archive.ubuntu.com/ubuntu noble InRelease [256 kB]
Get:2 http://security.ubuntu.com/ubuntu noble-security InRelease [126 kB]
Get:3 http://archive.ubuntu.com/ubuntu noble-updates InRelease [126 kB]
...

root@box:/# curl -I https://www.docker.com/
HTTP/2 200
cache-control: public, max-age=604800
content-type: text/html; charset=UTF-8
link: <https://www.docker.com/wp-json/>; rel="https://api.w.org/"
link: <https://www.docker.com/wp-json/wp/v2/pages/69521>; rel="alternate"; title="JSON"; type="application/json"
link: <https://www.docker.com/>; rel=shortlink
server: nginx
strict-transport-security: max-age=31622400
x-pantheon-styx-hostname: styx-us-a-679c859bf8-jbhhr
x-styx-req-id: af661076-2494-11f1-afe7-5e35eafe3e72
age: 70870
accept-ranges: bytes
via: 1.1 varnish, 1.1 varnish, 1.1 varnish, 1.1 varnish
x-content-type-options: nosniff
referrer-policy: strict-origin-when-cross-origin
permissions-policy: geolocation=(), microphone=(), camera=(), fullscreen=(self), payment=()
x-frame-options: SAMEORIGIN
x-xss-protection: 1; mode=block
date: Sat, 21 Mar 2026 15:21:55 GMT
x-served-by: cache-chi-kigq8000107-CHI, cache-dub4328-DUB, cache-dub4328-DUB, cache-dub4328-DUB
x-cache: HIT, HIT, MISS, MISS
x-cache-hits: 32, 26, 0, 0
x-timer: S1774106515.462653,VS0,VE4
vary: Accept-Encoding, X-Language-Locale, Cookie, Cookie
content-length: 348633

root@box:/# dig docker.com +short
23.185.0.4
```

### nginx

https://hub.docker.com/_/nginx

Exposing a web server on the host's port 8080.

```
> sudo go run ./box pull "docker.io/library/nginx:latest" ./build/images/nginx/runtime --quiet
> sudo go run ./box run --port 8080:80:tcp nginx-container ./build/images/nginx/runtime --quiet
```

On host:
```bash
> ip addr
...
3: virbr0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default qlen 1000
    link/ether 58:11:22:c0:c1:3a brd ff:ff:ff:ff:ff:ff
    inet 192.168.0.171/24 scope global virbr0
       valid_lft forever preferred_lft forever
    inet6 fe80::5a11:22ff:fec0:c13a/64 scope link proto kernel_ll
       valid_lft forever preferred_lft forever
76: bridge-box: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default qlen 1000
    link/ether 92:31:70:7f:ec:2a brd ff:ff:ff:ff:ff:ff
    inet 10.0.0.171/24 brd 10.0.0.255 scope global bridge-box
       valid_lft forever preferred_lft forever
    inet6 fe80::9031:70ff:fe7f:ec2a/64 scope link proto kernel_ll
       valid_lft forever preferred_lft forever
78: veth-box-host@if77: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue master bridge-box state UP group default qlen 1000
    link/ether fe:6c:55:ff:8e:00 brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 169.254.252.2/16 brd 169.254.255.255 scope global noprefixroute veth-box-host
       valid_lft forever preferred_lft forever
    inet6 fe80::fc6c:55ff:feff:8e00/64 scope link
       valid_lft forever preferred_lft forever3: virbr0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP group default qlen 1000
    link/ether 58:11:22:c0:c1:3a brd ff:ff:ff:ff:ff:ff
    inet 192.168.0.171/24 scope global virbr0
       valid_lft forever preferred_lft forever
    inet6 fe80::5a11:22ff:fec0:c13a/64 scope link proto kernel_ll
       valid_lft forever preferred_lft forever

> curl 10.0.0.172:80
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
<style>
html { color-scheme: light dark; }
body { width: 35em; margin: 0 auto;
font-family: Tahoma, Verdana, Arial, sans-serif; }
</style>
</head>
<body>
<h1>Welcome to nginx!</h1>
<p>If you see this page, nginx is successfully installed and working.
Further configuration is required for the web server, reverse proxy,
API gateway, load balancer, content cache, or other features.</p>

<p>For online documentation and support please refer to
<a href="https://nginx.org/">nginx.org</a>.<br/>
To engage with the community please visit
<a href="https://community.nginx.org/">community.nginx.org</a>.<br/>
For enterprise grade support, professional services, additional
security features and capabilities please refer to
<a href="https://f5.com/nginx">f5.com/nginx</a>.</p>

<p><em>Thank you for using nginx.</em></p>
</body>
</html>
```

From another device:
```bash
> curl 192.168.0.171:80

<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
<style>
html { color-scheme: light dark; }
body { width: 35em; margin: 0 auto;
font-family: Tahoma, Verdana, Arial, sans-serif; }
</style>
</head>
<body>
<h1>Welcome to nginx!</h1>
<p>If you see this page, nginx is successfully installed and working.
Further configuration is required for the web server, reverse proxy,
API gateway, load balancer, content cache, or other features.</p>

<p>For online documentation and support please refer to
<a href="https://nginx.org/">nginx.org</a>.<br/>
To engage with the community please visit
<a href="https://community.nginx.org/">community.nginx.org</a>.<br/>
For enterprise grade support, professional services, additional
security features and capabilities please refer to
<a href="https://f5.com/nginx">f5.com/nginx</a>.</p>

<p><em>Thank you for using nginx.</em></p>
</body>
</html>
```