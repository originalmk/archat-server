# 🔀 Archat
*This application in in alpha stage, some features may not be working correctly or there may be bugs*

Simple P2P server (and client - for testing purposes only, client for end user is written in Electron - you can
get it [there](https://github.com/DamianLaczynski/archat-client).

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

## Running using Docker
You are provided with Dockerfile inside this repository. You can use it to build your image and run archat-server
in a container. This image does not allow to run client, because it does not make sense.

0. Enter the top directory of this repository (using cd)
1. Start with building image
```bash
docker build -t archat-server .
```
2. Now run a container
```bash
docker run -d -p 8080-8081:8080-8081 archat-server
```
You may change exposed ports according to your needs, just remember that they need to be open. Also, be sure to build an image after any code update :)