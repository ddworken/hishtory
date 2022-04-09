FROM golang:1.17
COPY go.mod ./
COPY go.sum ./
RUN unset GOPATH; go mod download
COPY . ./
RUN unset GOPATH; cat VERSION; go build -o /server -ldflags "-X main.ReleaseVersion=v0.`cat VERSION`" server/server.go
CMD [ "/server" ]

