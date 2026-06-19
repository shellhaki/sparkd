# Manual Tests

Start SparkD first:

```bash
sudo ./main daemon
```

Then, from another terminal:

```bash
./manual-tests/create-pg.sh
./manual-tests/list.sh
./manual-tests/resume-pg.sh
./manual-tests/delete-pg.sh
```

Override the server URL when testing a remote daemon:

```bash
SPARKD_URL=http://server-ip:8721 ./manual-tests/list.sh
```
