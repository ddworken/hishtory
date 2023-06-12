FROM golang:1.20-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
ARG GOARCH
RUN GOARCH=${GOARCH} go build -o /server -ldflags "-X main.ReleaseVersion=v0.`cat VERSION`" backend/server/server.go

FROM golang:1.20-alpine
COPY --from=builder /server /server
CMD ["/server"]  
