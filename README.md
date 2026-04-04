# DockerDNS

DockerDNS is a small DNS server for Docker that resolves:

`<container-name>.<domain>`

to the IP address of the running Docker container with that exact name.

Example:

`sabnzbd.domain.local` -> IP of the `sabnzbd` container

It is intended for setups where Docker already has stable container names, but you want a private DNS suffix on top without adding labels, reverse proxies, or a full service discovery stack.

## What It Does

- Watches the Docker API on demand by container name
- Resolves exact single-label names such as `plex.domain.local`
- Returns the container IP from a preferred Docker network if configured
- Supports both UDP and TCP DNS on port `53`
- Returns `NXDOMAIN` when the name does not match a running container

## What It Does Not Do

- It does not watch Docker events or build a zone file
- It does not expose wildcard or multi-label routing
- It does not do reverse proxying
- It does not join multiple Docker hosts into one DNS namespace

## Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `DOMAIN` | `domain.local` | DNS suffix to match |
| `DOCKER_NETWORK` | empty | Preferred Docker network when a container has multiple IPs |
| `LISTEN_ADDR` | `:53` | Bind address for the DNS server |
| `TTL` | `30` | TTL for returned DNS records in seconds |

## Name Matching Rules

- Query names must exactly match `container.domain`
- Only one label before the domain is accepted
- `sabnzbd.domain.local` works
- `api.sabnzbd.domain.local` does not
- Matching is case-insensitive

## Run With Docker

```bash
docker run -d \
  --name dockerdns \
  -e DOMAIN=domain.local \
  -e DOCKER_NETWORK=appnet \
  -p 53:53/udp \
  -p 53:53/tcp \
  -v /var/run/docker.sock:/var/run/docker.sock \
  ghcr.io/kakatkarakshay/dockerdns:latest
```

## Run With Docker Compose

```yaml
services:
  dockerdns:
    image: ghcr.io/kakatkarakshay/dockerdns:latest
    container_name: dockerdns
    environment:
      DOMAIN: domain.local
      DOCKER_NETWORK: appnet
      TTL: "30"
    ports:
      - "53:53/udp"
      - "53:53/tcp"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    restart: unless-stopped
```

A ready-to-use example is also included in [`docker-compose.yml`](./docker-compose.yml).

## Example Queries

```bash
dig @127.0.0.1 sabnzbd.domain.local
dig @127.0.0.1 plex.domain.local
dig @127.0.0.1 AAAA jellyfin.domain.local
```

## How IP Selection Works

- If `DOCKER_NETWORK` is set and the container is attached to it, DockerDNS uses that network IP
- Otherwise it returns the first usable IP found across attached networks
- If the query is `A`, only IPv4 answers are returned
- If the query is `AAAA`, only IPv6 answers are returned

## Security Note

DockerDNS needs access to the Docker socket because it resolves names by inspecting containers at query time:

```text
/var/run/docker.sock
```

Treat it as a privileged component and run it only on trusted hosts.

## Build

```bash
go build .
docker build -t dockerdns:latest .
```

## Project Name

Repository: `KakatkarAkshay/dockerdns`
