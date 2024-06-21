FROM golang:1.22 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY *.go .
COPY common/ common/
COPY client/ client/
COPY server/ server/
RUN go build -v -o ./archat-server .

FROM ubuntu:noble
WORKDIR /app
COPY --from=build /app/archat-server /app/archat-server
CMD ["./archat-server", "--run", "server"]
