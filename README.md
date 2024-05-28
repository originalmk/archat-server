# 🔀 Archat
*It is not working application yet, work in progress!*

Simple P2P server (and client - for testing purposes only, client for end user will be written in NodeJS using Electron, soon).

## Starting
You can use these commands to start client and/or server:

```bash
go run . --run client --waddr X.X.X.X:Y --uaddr X.X.X.X:Y
# for example:
go run . --run client --waddr krzyzanowski.dev:8080 --uaddr krzyzanowski.dev:8081
```
Note that server should be started **before** running client.

```bash
go run . --run server --waddr X.X.X.X:Y --uaddr X.X.X.X:Y
# for example:
go run . --run server --waddr krzyzanowski.dev:8080 --uaddr krzyzanowski.dev:8081
```

`--waddr` and `--uaddr` options are optional, default values are respectively `:8080` and `:8081` (which is a short form of `localhost:8080` and `localhost:8081`)

`--run` option is mandatory and may take value of either `client` or `server`
