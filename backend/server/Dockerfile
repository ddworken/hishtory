FROM golang:1.18 AS builder
COPY go.mod ./
COPY go.sum ./
RUN unset GOPATH; go mod download
COPY . ./
RUN unset GOPATH; GOARCH=amd64 go build -o /server -ldflags "-X main.ReleaseVersion=v0.`cat VERSION`" backend/server/server.go

FROM golang:1.18
COPY --from=builder /server /server
CMD ["/server"]  
