# syntax=docker/dockerfile:1

# pkl-cli
FROM alpine/curl AS deps

ARG PKL_VERSION=0.26.1
RUN curl -L -o /usr/local/bin/pkl https://github.com/apple/pkl/releases/download/${PKL_VERSION}/pkl-alpine-linux-amd64 && chmod +x /usr/local/bin/pkl 

# phase-cli
FROM phasehq/cli:1.18.1 AS phase

# app builder
FROM golang:1.22-alpine AS builder
WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY *.go ./
RUN go build -v -o /usr/local/bin/app ./...

# runner
FROM docker:24

COPY --from=phase /usr/local/bin/phase /usr/local/bin/phase
COPY --from=phase /usr/local/bin/_internal /usr/local/bin/_internal
COPY --from=deps /usr/local/bin/pkl /usr/local/bin/pkl
COPY --from=builder /usr/local/bin/app /usr/local/bin/app

CMD ["app"]
