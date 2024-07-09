# syntax=docker/dockerfile:1
FROM golang:1.22 as builder
WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY *.go ./
RUN go build -v -o /usr/local/bin/app ./...

FROM docker:24
COPY --from=builder /usr/local/bin/app /usr/local/bin/app
CMD ["app"]
