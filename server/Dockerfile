FROM golang:1.17
COPY go.mod ./
COPY go.sum ./
RUN unset GOPATH; go mod download
COPY . ./
RUN unset GOPATH; go build -o /server server/server.go
CMD [ "/server" ]

