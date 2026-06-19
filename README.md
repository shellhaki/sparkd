# SparkD

**SparkD is a cell engine for databases.** Run it on your laptop, home server, or VPS, then create isolated PostgreSQL cells over a normal TCP API.

PostgreSQL support is first. SparkD imports PostgreSQL into the base rootfs once, then clones that imported base for each cell.

## What You Get

- TCP HTTP API on port `8721` by default
- `/create`, `/list`, `/pause`, `/resume`, `/monitor`, `/delete`, `/health`
- JSON responses for every daemon endpoint
- PostgreSQL credentials and connection strings from `/create`
- Per-cell rootfs, PID/network namespace, IP, and forwarded host port
- JSON metadata in `/var/lib/sparkd/cells.json`
- RAM/CPU enforcement with cgroup v2
- Disk enforcement with a quota-sized ext4 rootfs image per cell

## Build

```bash
go build -o sparkd .
```

SparkD needs root for `chroot`, namespaces, veth devices, bridge setup, cgroups, loop-mounted rootfs images, and iptables.

Build on a VPS:

```bash
ssh user@server-ip
git clone <sparkd-repo-url>
cd sparkd
go build -o sparkd .
sudo ./sparkd daemon
```

## Run

```bash
sudo ./sparkd daemon
```

Default server:

```text
0.0.0.0:8721
```

Use a different address or port if `8721` is busy:

```bash
sudo SPARKD_ADDR=0.0.0.0:8877 ./sparkd daemon
```

Set `SPARKD_HOST` if returned database connection strings should use a specific public IP or DNS name:

```bash
sudo SPARKD_HOST=db.example.com ./sparkd daemon
```

Open the daemon port on your VPS firewall/security group. For quick local-only testing, run with `SPARKD_ADDR=127.0.0.1:8721`.

## systemd

Example unit:

```ini
[Unit]
Description=SparkD database cell daemon
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/sparkd daemon
Environment=SPARKD_ADDR=0.0.0.0:8721
Environment=SPARKD_HOST=server-ip-or-dns
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
```

Install:

```bash
sudo cp sparkd /usr/local/bin/sparkd
sudo cp sparkd.service /etc/systemd/system/sparkd.service
sudo systemctl daemon-reload
sudo systemctl enable --now sparkd
```

## API

Base URL:

```text
http://server-ip:8721
```

Health:

```bash
curl http://127.0.0.1:8721/health
```

Create a Postgres cell:

```bash
curl -X POST http://127.0.0.1:8721/create \
  -H 'content-type: application/json' \
  -d '{"name":"app-db","dbtype":"pg","port":3001,"ram":"128mb","disk":"512mb"}'
```

Response includes cell metadata, progress events, credentials, and the connection string:

```json
{
  "connection_string": "postgres://postgres:...@server-ip:3001/app_db?sslmode=disable"
}
```

List cells:

```bash
curl http://127.0.0.1:8721/list
```

Monitor one cell:

```bash
curl 'http://127.0.0.1:8721/monitor?name=app-db'
```

Pause one cell:

```bash
curl -X POST http://127.0.0.1:8721/pause \
  -H 'content-type: application/json' \
  -d '{"name":"app-db"}'
```

Resume one cell:

```bash
curl -X POST http://127.0.0.1:8721/resume \
  -H 'content-type: application/json' \
  -d '{"name":"app-db"}'
```

Delete one cell:

```bash
curl -X POST http://127.0.0.1:8721/delete \
  -H 'content-type: application/json' \
  -d '{"name":"app-db"}'
```

## Import PostgreSQL Once

The daemon imports automatically on first `/create`, but you can warm it manually:

```bash
sudo ./sparkd import
```

## Manual Tests

```bash
sudo ./main daemon
./manual-tests/create-pg.sh
./manual-tests/list.sh
./manual-tests/delete-pg.sh
```

For a remote daemon:

```bash
SPARKD_URL=http://server-ip:8721 ./manual-tests/list.sh
```

## Code Examples

Client examples live in [examples](./examples):

- [Go](./examples/go.md)
- [Rust](./examples/rust.md)
- [TypeScript](./examples/typescript.md)
- [Zig](./examples/zig.md)
- [Odin](./examples/odin.md)

## State

Defaults:

- State directory: `/var/lib/sparkd`
- Base rootfs: `/var/lib/sparkd/base/rootfs`
- Cell directory: `/var/lib/sparkd/cells`
- Metadata store: `/var/lib/sparkd/cells.json`

Override state for local experiments:

```bash
sudo SPARKD_STATE_DIR=/tmp/sparkd-state SPARKD_ADDR=127.0.0.1:8721 ./sparkd daemon
```
