FROM golang:1.23.0-alpine3.20 AS builder

WORKDIR /app
RUN apk add --update --no-cache --virtual .build-deps build-base 
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
ARG GOARCH
RUN GOARCH=${GOARCH} go build -o /server -ldflags "-X main.ReleaseVersion=v0.`cat VERSION`" backend/server/server.go && \
    apk del .build-deps

FROM alpine:3.17
COPY --from=builder /server /server
CMD ["/server"]  
